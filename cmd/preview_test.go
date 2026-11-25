package cmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"sigs.k8s.io/yaml"
)

func TestPreview_WritesPlaybook(t *testing.T) {
	repo := gitRepo(t, map[string]string{
		"Chart.yaml":                         copyFile(t, "../pkg/gen/testdata/reagent-redis/Chart.yaml"),
		"values.yaml":                        copyFile(t, "../pkg/gen/testdata/reagent-redis/values.yaml"),
		"docs/antora.yml":                    "name: reagent-redis\ntitle: Redis\nversion: ~\nnav:\n- modules/ROOT/nav.adoc\n",
		"docs/modules/ROOT/nav.adoc":         "* xref:index.adoc[Overview]\n",
		"docs/modules/ROOT/pages/index.adoc": "= Redis\n",
	})
	out := t.TempDir()
	var gotDir string
	var gotArgs []string
	runner := func(_ context.Context, dir string, args ...string) error {
		gotDir, gotArgs = dir, args
		return nil
	}
	rootDocs := fstest.MapFS{
		"docs/antora.yml":                       {Data: []byte("name: ROOT\nversion: ~\n")},
		"docs/modules/ROOT/pages/index.adoc":    {Data: []byte("= Home\n")},
		"docs/modules/ROOT/pages/concepts.adoc": {Data: []byte("= Concepts\n")},
		"components.yaml":                       {Data: []byte("org: helmetica-framework\ncomponents:\n- {repo: azoth, group: Building reagents, kind: chart, enabled: false}\n- {repo: reagent-redis, group: Platform, kind: tool}\n")},
	}
	if err := runPreview(context.Background(), previewOptions{RepoDir: repo, Out: out, Run: runner, RootDocs: rootDocs}, os.Stderr); err != nil {
		t.Fatal(err)
	}
	if gotDir != out || !slices.Contains(gotArgs, "--log-failure-level=warn") || !slices.Contains(gotArgs, filepath.Join(out, "playbook.yml")) {
		t.Errorf("runner called with %s %v", gotDir, gotArgs)
	}
	raw, err := os.ReadFile(filepath.Join(out, "playbook.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var pb struct {
		Content struct {
			Sources []struct {
				URL        string   `json:"url"`
				StartPath  string   `json:"start_path"`
				StartPaths []string `json:"start_paths"`
			} `json:"sources"`
		} `json:"content"`
	}
	if err := yaml.Unmarshal(raw, &pb); err != nil {
		t.Fatal(err)
	}
	s := pb.Content.Sources
	// Listed components get index-only stubs so xrefs to them resolve;
	// the previewed component itself is not stubbed.
	if len(s) != 2 || s[0].URL != repo || s[0].StartPath != "docs" ||
		!slices.Equal(s[1].StartPaths, []string{"root", "generated/reagent-redis", "stubs/azoth"}) {
		t.Fatalf("sources = %+v", s)
	}
	if _, err := os.Stat(filepath.Join(out, "generated/reagent-redis/modules/ROOT/pages/references/parameters.adoc")); err != nil {
		t.Error("reagent kind not detected from Chart.yaml:", err)
	}
	// The framework pages are the real ones, so xrefs into ROOT resolve.
	for _, p := range []string{"concepts.adoc", "reagents.adoc", "index.adoc"} {
		if _, err := os.Stat(filepath.Join(out, "root/modules/ROOT/pages", p)); err != nil {
			t.Errorf("ROOT page %s missing", p)
		}
	}
}

func TestPreview_NoDocs(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "x"})
	err := runPreview(context.Background(), previewOptions{RepoDir: repo, Out: t.TempDir(), RootDocs: fstest.MapFS{}, Run: func(context.Context, string, ...string) error { return nil }}, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "no docs/antora.yml") {
		t.Fatalf("want no docs error, got %v", err)
	}
}

func TestAntoraCommand(t *testing.T) {
	name, args := antoraCommand("none", "/out", "/repo", "/out/playbook.yml")
	if name != "npx" || !slices.Contains(args, "antora@"+antoraNPMVersion) {
		t.Errorf("npx fallback = %s %v", name, args)
	}
	name, args = antoraCommand("docker", "/out", "/repo", "/out/playbook.yml")
	if name != "docker" || !slices.Contains(args, "/repo:/repo") || !slices.Contains(args, antoraImage) {
		t.Errorf("docker = %s %v", name, args)
	}
}

func TestPreview_KindOverride(t *testing.T) {
	repo := gitRepo(t, map[string]string{
		"Chart.yaml":                         "apiVersion: v2\nname: azoth\ntype: library\nversion: 0.0.1\n",
		"values.yaml":                        "backup:\n  enabled: false\n",
		"docs/antora.yml":                    "name: azoth\nversion: ~\n",
		"docs/modules/ROOT/pages/index.adoc": "= Azoth\n",
	})
	out := t.TempDir()
	err := runPreview(context.Background(), previewOptions{RepoDir: repo, Out: out, Kind: "chart", RootDocs: fstest.MapFS{"docs/antora.yml": {Data: []byte("name: ROOT\nversion: ~\n")}},
		Run: func(context.Context, string, ...string) error { return nil }}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "generated", "azoth")); err == nil {
		t.Error("--kind chart must not generate a Parameters page")
	}
}
