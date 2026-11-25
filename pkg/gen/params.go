// Package gen derives reference documentation from charts and CRDs.
package gen

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/helmetica-framework/chrysopoeia/pkg/schemagen"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	"github.com/helmetica-framework/rosarium/pkg/schemadoc"
)

// azoth is the framework library chart; every other dependency of a reagent
// is its prima materia.
const azoth = "azoth"

// ReagentDoc is what the docs say about a reagent's end-user API.
type ReagentDoc struct {
	Group, Version, Kind string
	Fields               []schemadoc.Field
	// Example is a minimal instance manifest.
	Example                                         string
	ChartVersion, PrimaMateria, PrimaMateriaVersion string
}

// Reagent loads the chart in chartDir and derives its API exactly the way
// chrysopoeia does when it generates the instance CRD.
func Reagent(chartDir string) (ReagentDoc, error) {
	chart, err := loader.LoadDir(chartDir)
	if err != nil {
		return ReagentDoc{}, fmt.Errorf("loading chart %s: %w", chartDir, err)
	}
	var opts []schemagen.GenerateOption
	if chart.Metadata.Annotations[schemagen.CRDKindAnnotation] == "" {
		// The platform names the CRD in its ClaimDefinition; without a chart
		// annotation, document the conventional name: the chart name in title case.
		kind := titleCase(chart.Name())
		opts = append(opts, schemagen.WithNames(apiextv1.CustomResourceDefinitionNames{
			Kind: kind, ListKind: kind + "List", Singular: strings.ToLower(kind), Plural: strings.ToLower(kind) + "s",
		}))
	}
	crd, err := schemagen.GenerateCRD(*chart, opts...)
	if err != nil {
		return ReagentDoc{}, fmt.Errorf("generating CRD for %s: %w", chartDir, err)
	}
	if len(crd.Spec.Versions) == 0 || crd.Spec.Versions[0].Schema == nil || crd.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
		return ReagentDoc{}, fmt.Errorf("generated CRD for %s has no schema", chartDir)
	}
	v := crd.Spec.Versions[0]
	values := v.Schema.OpenAPIV3Schema.Properties["spec"].Properties["values"]

	d := ReagentDoc{
		Group:        crd.Spec.Group,
		Version:      v.Name,
		Kind:         crd.Spec.Names.Kind,
		Fields:       withDefaults(schemadoc.Flatten(values, ""), chart.Values),
		ChartVersion: chart.Metadata.Version,
	}
	for _, dep := range chart.Metadata.Dependencies {
		if dep.Name != azoth {
			d.PrimaMateria, d.PrimaMateriaVersion = dep.Name, dep.Version
			break
		}
	}

	example := map[string]any{
		"apiVersion": d.Group + "/" + d.Version,
		"kind":       d.Kind,
		"metadata":   map[string]any{"name": "my-" + chart.Name(), "namespace": "my-app"},
		"spec": map[string]any{
			"approval": map[string]any{"strategy": "Automatic"},
			"values":   map[string]any{},
		},
	}
	b, err := yaml.Marshal(example)
	if err != nil {
		return ReagentDoc{}, err
	}
	d.Example = string(b)
	return d, nil
}

func titleCase(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, "")
}

// withDefaults fills Default from the chart's values.yaml where the
// generated schema carries none: values.yaml is the default.
func withDefaults(fields []schemadoc.Field, values map[string]any) []schemadoc.Field {
	for i, f := range fields {
		if f.Default != "" || strings.Contains(f.Path, "[]") {
			continue
		}
		var cur any = values
		for _, k := range strings.Split(f.Path, ".") {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = m[k]
		}
		switch v := cur.(type) {
		case nil, map[string]any:
		case string:
			if v == "" {
				fields[i].Default = `""`
			} else {
				fields[i].Default = v
			}
		default:
			if b, err := json.Marshal(v); err == nil {
				fields[i].Default = string(b)
			}
		}
	}
	return fields
}
