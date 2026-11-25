package schemadoc

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

var update = flag.Bool("update", false, "rewrite golden files")

func schema(t *testing.T, js string) apiextv1.JSONSchemaProps {
	t.Helper()
	var s apiextv1.JSONSchemaProps
	if err := json.Unmarshal([]byte(js), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func byPath(fields []Field, path string) (Field, bool) {
	i := slices.IndexFunc(fields, func(f Field) bool { return f.Path == path })
	if i < 0 {
		return Field{}, false
	}
	return fields[i], true
}

func TestFlatten_Nested(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{
		"backup":{"type":"object","properties":{"mode":{"type":"string","enum":["Schedule","BucketOnly"],"default":"Schedule"}}},
		"network":{"type":"object","description":"Network settings.","properties":{"expose":{"type":"boolean"}}}}}`)
	fields := Flatten(s, "")
	f, ok := byPath(fields, "backup.mode")
	if !ok {
		t.Fatalf("backup.mode missing: %+v", fields)
	}
	if f.Type != "enum: Schedule, BucketOnly" || f.Default != "Schedule" {
		t.Errorf("backup.mode = %+v", f)
	}
	if _, ok := byPath(fields, "backup"); ok {
		t.Error("object without description must not get its own row")
	}
	if f, ok := byPath(fields, "network"); !ok || f.Type != "object" {
		t.Errorf("object with description must get a row, got %+v %v", f, ok)
	}
}

func TestFlatten_Prefix(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{"a":{"type":"string"}}}`)
	if _, ok := byPath(Flatten(s, "spec"), "spec.a"); !ok {
		t.Error("prefix not applied")
	}
}

func TestFlatten_ArrayOfObjects(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{"rules":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"}}}}}}`)
	fields := Flatten(s, "")
	if f, ok := byPath(fields, "rules"); !ok || f.Type != "[]object" {
		t.Errorf("rules = %+v %v", f, ok)
	}
	if _, ok := byPath(fields, "rules[].name"); !ok {
		t.Errorf("rules[].name missing: %+v", fields)
	}
}

func TestFlatten_PreserveUnknown(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{"config":{"type":"object","x-kubernetes-preserve-unknown-fields":true,"properties":{"x":{"type":"string"}}}}}`)
	fields := Flatten(s, "")
	if f, ok := byPath(fields, "config"); !ok || f.Type != "object (free-form)" {
		t.Errorf("config = %+v %v", f, ok)
	}
	if _, ok := byPath(fields, "config.x"); ok {
		t.Error("free-form object must not list children")
	}
}

func TestFlatten_Map(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{"args":{"type":"object","additionalProperties":{"type":"string"}}}}`)
	if f, ok := byPath(Flatten(s, ""), "args"); !ok || f.Type != "map[string]string" {
		t.Errorf("args = %+v %v", f, ok)
	}
}

func TestFlatten_RequiredAndValidations(t *testing.T) {
	s := schema(t, `{"type":"object","required":["type"],"properties":{
		"type":{"type":"string","minLength":1,"maxLength":63,"pattern":"^[a-z]+$"},
		"replicas":{"type":"integer","minimum":1,"maximum":3,"x-kubernetes-validations":[{"rule":"self == oldSelf","message":"replicas is immutable"}]}}}`)
	fields := Flatten(s, "")
	ty, _ := byPath(fields, "type")
	if !ty.Required {
		t.Error("type must be required")
	}
	for _, want := range []string{"min length 1", "max length 63", "pattern `pass:c[^[a-z\\]+$]`"} {
		if !slices.Contains(ty.Validations, want) {
			t.Errorf("type validations %v missing %q", ty.Validations, want)
		}
	}
	r, _ := byPath(fields, "replicas")
	for _, want := range []string{"minimum 1", "maximum 3", "replicas is immutable"} {
		if !slices.Contains(r.Validations, want) {
			t.Errorf("replicas validations %v missing %q", r.Validations, want)
		}
	}
}

func TestFlatten_DeepNestingTerminates(t *testing.T) {
	js := `{"type":"string"}`
	for range 100 {
		js = `{"type":"object","properties":{"n":` + js + `}}`
	}
	fields := Flatten(schema(t, js), "")
	if len(fields) == 0 || len(fields) > 100 {
		t.Fatalf("unexpected field count %d", len(fields))
	}
}

func TestCode_EscapesClosingBracket(t *testing.T) {
	if got := Code("a]b"); got != "`pass:c[a\\]b]`" {
		t.Errorf("Code = %s", got)
	}
}

func TestProse(t *testing.T) {
	in := "Who takes the backups.\n  Schedule - k8up\n  BucketOnly - none\n\nWrapped line\ncontinues here."
	want := "Who takes the backups. +\nSchedule - k8up +\nBucketOnly - none +\nWrapped line continues here."
	if got := prose(in); got != want {
		t.Errorf("prose =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderTable_EscapesPipe(t *testing.T) {
	var b bytes.Buffer
	if err := RenderTable(&b, []Field{{Path: "a", Type: "string", Description: "x | y"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `x \| y`) {
		t.Errorf("pipe not escaped:\n%s", b.String())
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s differs (run with -update):\n%s", name, got)
	}
}

func TestRenderTable_GoldenValues(t *testing.T) {
	raw, err := os.ReadFile("testdata/values.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := RenderTable(&b, Flatten(schema(t, string(raw)), "")); err != nil {
		t.Fatal(err)
	}
	golden(t, "values.golden.adoc", b.Bytes())
}

func TestRenderTable_GoldenCRD(t *testing.T) {
	raw, err := os.ReadFile("testdata/adept-action.crd.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextv1.CustomResourceDefinition
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	if err := RenderTable(&b, Flatten(spec, "spec")); err != nil {
		t.Fatal(err)
	}
	golden(t, "adept-action.golden.adoc", b.Bytes())
}

func TestRenderTable_EscapesAttributeReferences(t *testing.T) {
	var b bytes.Buffer
	fields := []Field{{Path: "a", Type: "enum: {x}", Description: "Secret is called {prefix}-creds", Validations: []string{"must match {y}"}}}
	if err := RenderTable(&b, fields); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`\{prefix}-creds`, `enum: \{x}`, `must match \{y}`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in:\n%s", want, b.String())
		}
	}
}

func TestFlatten_MapOfObjects(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{"valueMapping":{"type":"object","additionalProperties":{"type":"object","properties":{"key":{"type":"string"},"name":{"type":"string"}}}}}}`)
	fields := Flatten(s, "spec")
	if f, ok := byPath(fields, "spec.valueMapping"); !ok || f.Type != "map[string]object" {
		t.Errorf("valueMapping = %+v %v", f, ok)
	}
	for _, p := range []string{"spec.valueMapping.<key>.key", "spec.valueMapping.<key>.name"} {
		if _, ok := byPath(fields, p); !ok {
			t.Errorf("%s missing: %+v", p, fields)
		}
	}
}

func TestFlatten_ArrayOfArraysOfObjects(t *testing.T) {
	s := schema(t, `{"type":"object","properties":{"m":{"type":"array","items":{"type":"array","items":{"type":"object","properties":{"x":{"type":"string"}}}}}}}`)
	if _, ok := byPath(Flatten(s, ""), "m[][].x"); !ok {
		t.Errorf("m[][].x missing: %+v", Flatten(s, ""))
	}
}

func TestEscapeAttr_LeavesCodeAlone(t *testing.T) {
	in := "a {b} " + Code("x{1}]y") + " {c}"
	want := `a \{b} ` + Code("x{1}]y") + ` \{c}`
	if got := escapeAttr(in); got != want {
		t.Errorf("escapeAttr =\n%s\nwant\n%s", got, want)
	}
}
