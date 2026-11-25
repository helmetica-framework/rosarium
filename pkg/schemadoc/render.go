// Package schemadoc turns OpenAPI v3 schemas (CRDs, generated values schemas)
// into AsciiDoc reference tables.
package schemadoc

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// maxDepth bounds recursion; real schemas are far shallower.
const maxDepth = 32

// Field is one row of a reference table.
type Field struct {
	Path        string
	Type        string
	Default     string
	Description string
	Required    bool
	Validations []string
}

// Flatten walks s and returns one Field per leaf (and per described object),
// with dotted paths below prefix. Arrays add "[]" to the path.
func Flatten(s apiextv1.JSONSchemaProps, prefix string) []Field {
	var out []Field
	walkProperties(&out, s, prefix, 0)
	return out
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func walkProperties(out *[]Field, s apiextv1.JSONSchemaProps, prefix string, depth int) {
	if depth >= maxDepth {
		return
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		walk(out, s.Properties[n], join(prefix, n), slices.Contains(s.Required, n), depth+1)
	}
}

func walk(out *[]Field, s apiextv1.JSONSchemaProps, path string, required bool, depth int) {
	f := Field{
		Path:        path,
		Type:        typeOf(s),
		Default:     defaultOf(s),
		Description: s.Description,
		Required:    required,
		Validations: validationsOf(s),
	}
	free := s.XPreserveUnknownFields != nil && *s.XPreserveUnknownFields
	switch {
	case free:
		*out = append(*out, f)
	case len(s.Properties) > 0 && depth >= maxDepth:
		*out = append(*out, f) // too deep: stop listing children
	case len(s.Properties) > 0:
		if f.Description != "" || f.Required || len(f.Validations) > 0 {
			*out = append(*out, f)
		}
		walkProperties(out, s, path, depth)
	case s.Type == "array" && s.Items != nil && s.Items.Schema != nil:
		*out = append(*out, f)
		items, p := *s.Items.Schema, path+"[]"
		for items.Type == "array" && items.Items != nil && items.Items.Schema != nil && depth < maxDepth {
			items, p, depth = *items.Items.Schema, p+"[]", depth+1
		}
		if len(items.Properties) > 0 {
			walkProperties(out, items, p, depth)
		}
	case s.AdditionalProperties != nil && s.AdditionalProperties.Schema != nil && len(s.AdditionalProperties.Schema.Properties) > 0:
		*out = append(*out, f)
		walkProperties(out, *s.AdditionalProperties.Schema, path+".<key>", depth)
	default:
		*out = append(*out, f)
	}
}

func typeOf(s apiextv1.JSONSchemaProps) string {
	if s.XPreserveUnknownFields != nil && *s.XPreserveUnknownFields {
		return "object (free-form)"
	}
	if len(s.Enum) > 0 {
		vals := make([]string, 0, len(s.Enum))
		for _, e := range s.Enum {
			var v any
			if err := json.Unmarshal(e.Raw, &v); err == nil {
				vals = append(vals, fmt.Sprint(v))
			} else {
				vals = append(vals, string(e.Raw))
			}
		}
		return "enum: " + strings.Join(vals, ", ")
	}
	switch {
	case s.Type == "array" && s.Items != nil && s.Items.Schema != nil:
		return "[]" + typeOf(*s.Items.Schema)
	case s.Type == "object" && len(s.Properties) == 0 && s.AdditionalProperties != nil && s.AdditionalProperties.Schema != nil:
		return "map[string]" + typeOf(*s.AdditionalProperties.Schema)
	case s.XIntOrString:
		return "int or string"
	case s.Type == "":
		return "any"
	}
	return s.Type
}

func defaultOf(s apiextv1.JSONSchemaProps) string {
	if s.Default == nil || len(s.Default.Raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(s.Default.Raw, &v); err != nil {
		return string(s.Default.Raw)
	}
	if str, ok := v.(string); ok {
		if str == "" {
			return `""`
		}
		return str
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func validationsOf(s apiextv1.JSONSchemaProps) []string {
	var v []string
	if s.Minimum != nil {
		v = append(v, fmt.Sprintf("minimum %g", *s.Minimum))
	}
	if s.Maximum != nil {
		v = append(v, fmt.Sprintf("maximum %g", *s.Maximum))
	}
	if s.MinLength != nil {
		v = append(v, fmt.Sprintf("min length %d", *s.MinLength))
	}
	if s.MaxLength != nil {
		v = append(v, fmt.Sprintf("max length %d", *s.MaxLength))
	}
	if s.MinItems != nil {
		v = append(v, fmt.Sprintf("min items %d", *s.MinItems))
	}
	if s.MaxItems != nil {
		v = append(v, fmt.Sprintf("max items %d", *s.MaxItems))
	}
	if s.Pattern != "" {
		v = append(v, "pattern "+Code(s.Pattern))
	}
	for _, r := range s.XValidations {
		if r.Message != "" {
			v = append(v, r.Message)
		} else {
			v = append(v, "rule "+Code(r.Rule))
		}
	}
	return v
}

// prose re-flows wrapped comment text: lines are joined with spaces, while
// blank lines and lines that start a list item (indented, "-", "*") keep a
// hard line break.
func prose(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var b strings.Builder
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if i > 0 {
			prev := strings.TrimSpace(lines[i-1])
			switch {
			case t == "" || prev == "":
				if t == "" {
					continue
				}
				b.WriteString(" +\n")
			case strings.HasPrefix(l, " ") || strings.HasPrefix(t, "-") || strings.HasPrefix(t, "*"):
				b.WriteString(" +\n")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString(t)
	}
	return b.String()
}

// escapeAttr keeps "{name}" in prose from being read as an AsciiDoc
// attribute reference (fatal at --log-failure-level=warn).
// Text inside Code spans is left alone: the pass macro already shows it verbatim.
func escapeAttr(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		loc := codeSpan.FindStringIndex(s)
		if loc == nil {
			b.WriteString(strings.ReplaceAll(s, "{", `\{`))
			break
		}
		b.WriteString(strings.ReplaceAll(s[:loc[0]], "{", `\{`))
		b.WriteString(s[loc[0]:loc[1]])
		s = s[loc[1]:]
	}
	return b.String()
}

var codeSpan = regexp.MustCompile("`pass:c\\[(?:\\\\\\]|[^\\]])*\\]`")

// cell escapes AsciiDoc table separators.
func cell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// Code renders s as inline monospace with no AsciiDoc substitutions other
// than HTML escaping, so regexes and CEL rules show verbatim.
func Code(s string) string {
	if s == "" {
		return ""
	}
	return "`pass:c[" + strings.ReplaceAll(s, "]", `\]`) + "]`"
}

// RenderTable writes fields as an AsciiDoc table.
func RenderTable(w io.Writer, fields []Field) error {
	var b strings.Builder
	b.WriteString("[cols=\"3,2,2,5\",options=\"header\"]\n|===\n|Path |Type |Default |Description\n")
	for _, f := range fields {
		path := Code(f.Path)
		if f.Required {
			path += " *(required)*"
		}
		desc := cell(escapeAttr(prose(f.Description)))
		if len(f.Validations) > 0 {
			if desc != "" {
				desc += " +\n"
			}
			desc += "_Validation:_ " + cell(escapeAttr(strings.Join(f.Validations, "; ")))
		}
		fmt.Fprintf(&b, "\n|%s\n|%s\n|%s\n|%s\n", cell(path), cell(escapeAttr(f.Type)), cell(Code(f.Default)), desc)
	}
	b.WriteString("|===\n")
	_, err := io.WriteString(w, b.String())
	return err
}
