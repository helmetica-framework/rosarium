package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "components.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_Valid(t *testing.T) {
	p := write(t, `org: helmetica-framework
reagentTopic: helmetica-reagent
components:
- repo: adept
  group: Operators
  kind: operator
- repo: alembic
  branch: master
  group: Platform
  kind: tool
  enabled: false
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "helmetica-framework" || c.ReagentTopic != "helmetica-reagent" {
		t.Fatalf("unexpected header: %+v", c)
	}
	if len(c.Components) != 2 {
		t.Fatalf("want 2 components, got %d", len(c.Components))
	}
	a := c.Components[0]
	if a.Repo != "adept" || a.Branch != "main" || !a.Enabled || a.Group != GroupOperators || a.Kind != KindOperator {
		t.Errorf("defaults not applied: %+v", a)
	}
	b := c.Components[1]
	if b.Branch != "master" || b.Enabled {
		t.Errorf("explicit values lost: %+v", b)
	}
}

func TestLoad_UnknownGroup(t *testing.T) {
	p := write(t, "org: x\ncomponents:\n- repo: a\n  group: Foo\n  kind: tool\n")
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), `unknown group "Foo"`) {
		t.Fatalf("want unknown group error, got %v", err)
	}
}

func TestLoad_ReagentsGroupNotAllowedForComponents(t *testing.T) {
	p := write(t, "org: x\ncomponents:\n- repo: a\n  group: Reagents\n  kind: tool\n")
	if _, err := Load(p); err == nil {
		t.Fatal("want error: Reagents group is reserved for discovered reagents")
	}
}

func TestLoad_DuplicateRepo(t *testing.T) {
	p := write(t, "org: x\ncomponents:\n- repo: a\n  group: Platform\n  kind: tool\n- repo: a\n  group: Platform\n  kind: tool\n")
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

func TestGroupOrder(t *testing.T) {
	want := []Group{"Framework", "Reagents", "Building reagents", "Operators", "Platform"}
	for i, g := range want {
		if GroupOrder[i] != g {
			t.Fatalf("GroupOrder[%d] = %q, want %q", i, GroupOrder[i], g)
		}
	}
}
