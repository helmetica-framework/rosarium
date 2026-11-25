package gen

import (
	"slices"
	"strings"
	"testing"

	"github.com/helmetica-framework/rosarium/pkg/schemadoc"
)

func field(fields []schemadoc.Field, path string) (schemadoc.Field, bool) {
	i := slices.IndexFunc(fields, func(f schemadoc.Field) bool { return f.Path == path })
	if i < 0 {
		return schemadoc.Field{}, false
	}
	return fields[i], true
}

func TestReagent_Redis(t *testing.T) {
	d, err := Reagent("testdata/reagent-redis")
	if err != nil {
		t.Fatal(err)
	}
	if d.Group != "v0.redis.helmetica-bundles.io" || d.Kind != "Redis" || d.Version != "bundle" {
		t.Errorf("GVK = %s/%s %s", d.Group, d.Version, d.Kind)
	}
	if d.ChartVersion != "0.1.0" || d.PrimaMateria != "redis-ha" || d.PrimaMateriaVersion != "4.39.1" {
		t.Errorf("chart metadata = %+v", d)
	}
	f, ok := field(d.Fields, "service.plan")
	if !ok || f.Default != "standard-1" {
		t.Errorf("service.plan = %+v %v", f, ok)
	}
	if _, ok := field(d.Fields, "redis.replicas"); ok {
		t.Error("non-exported value redis.replicas listed")
	}
	for _, want := range []string{
		"apiVersion: v0.redis.helmetica-bundles.io/bundle",
		"kind: Redis",
		"  approval:\n    strategy: Automatic",
	} {
		if !strings.Contains(d.Example, want) {
			t.Errorf("Example missing %q:\n%s", want, d.Example)
		}
	}
}

func TestReagent_DefaultsFromValues(t *testing.T) {
	d, err := Reagent("testdata/reagent-redis")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"backup.enabled": "true", "service.storage": "10Gi", "network.allowedNamespaces": "[]"} {
		if f, ok := field(d.Fields, path); !ok || f.Default != want {
			t.Errorf("%s default = %q (found %v), want %q", path, f.Default, ok, want)
		}
	}
}

func TestTitleCase(t *testing.T) {
	if got := titleCase("my-cache"); got != "MyCache" {
		t.Errorf("titleCase = %q", got)
	}
}

func TestReagent_NoExports(t *testing.T) {
	d, err := Reagent("testdata/no-exports")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Fields) != 0 {
		t.Errorf("want no fields, got %+v", d.Fields)
	}
	if d.PrimaMateria != "" {
		t.Errorf("no dependencies, PrimaMateria = %q", d.PrimaMateria)
	}
}

func TestReagent_MissingDir(t *testing.T) {
	if _, err := Reagent("testdata/does-not-exist"); err == nil {
		t.Fatal("want error")
	}
}
