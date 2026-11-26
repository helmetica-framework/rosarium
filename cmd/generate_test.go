package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/helmetica-framework/rosarium/pkg/discover"
)

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func copyFile(t *testing.T, from string) string {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixture builds a rosarium root, one operator and one reagent repo.
func fixture(t *testing.T, operatorDocs bool) (opts generateOptions) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"components.yaml":                    "org: helmetica-framework\nreagentTopic: helmetica-reagent\ncomponents:\n- {repo: adept, group: Operators, kind: operator}\n- {repo: cupel, group: Building reagents, kind: tool, enabled: false}\n",
		"docs/antora.yml":                    "name: ROOT\ntitle: Helmetica Framework\nversion: ~\nnav:\n- modules/ROOT/nav.adoc\n",
		"docs/modules/ROOT/nav.adoc":         "* xref:index.adoc[Home]\n",
		"docs/modules/ROOT/pages/index.adoc": "= Helmetica\n",
		"ui/css/helmetica.css":               "",
	}
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(c), 0o644)
	}
	opFiles := map[string]string{
		"config/crd/bases/adept.helmetica.io_actions.yaml": copyFile(t, "../pkg/gen/testdata/operator/config/crd/bases/adept.helmetica.io_actions.yaml"),
	}
	if operatorDocs {
		opFiles["docs/antora.yml"] = "name: adept\ntitle: Adept\nversion: ~\n"
		opFiles["docs/modules/ROOT/pages/index.adoc"] = "= Adept\n"
	}
	redis := gitRepo(t, map[string]string{
		"Chart.yaml":                         copyFile(t, "../pkg/gen/testdata/reagent-redis/Chart.yaml"),
		"values.yaml":                        copyFile(t, "../pkg/gen/testdata/reagent-redis/values.yaml"),
		"docs/antora.yml":                    "name: reagent-redis\ntitle: Redis\nversion: ~\n",
		"docs/modules/ROOT/pages/index.adoc": "= Redis\n",
	})
	return generateOptions{
		Root:       root,
		Config:     filepath.Join(root, "components.yaml"),
		Out:        filepath.Join(root, "build"),
		CacheDir:   filepath.Join(root, ".cache"),
		NoDiscover: true,
		Locals:     map[string]string{"adept": gitRepo(t, opFiles), "reagent-redis": redis},
	}
}

func TestGenerate_LocalOnly(t *testing.T) {
	opts := fixture(t, true)
	var log bytes.Buffer
	if err := runGenerate(context.Background(), opts, &log); err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	for _, p := range []string{
		"playbook.yml",
		"root/antora.yml",
		"root/modules/ROOT/pages/reagents.adoc",
		"ui/css/helmetica.css",
		"ui/partials/nav-explore.hbs",
		"root/modules/ROOT/partials/components.adoc",
		"generated/reagent-redis/modules/ROOT/pages/references/parameters.adoc",
		"generated/adept/modules/ROOT/pages/references/api.adoc",
		".git",
	} {
		if _, err := os.Stat(filepath.Join(opts.Out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	pb := copyFile(t, filepath.Join(opts.Out, "playbook.yml"))
	// Antora resolves relative URLs against the playbook dir only with a "./" prefix.
	for _, l := range strings.Split(pb, "\n") {
		if u, ok := strings.CutPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- ")), "url: "); ok && !strings.HasPrefix(u, "./") && !strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "http") && u != "." {
			t.Errorf("relative source url without ./ prefix: %s", u)
		}
	}
	if strings.Contains(pb, "cupel") {
		t.Error("disabled component in playbook")
	}
	cat := copyFile(t, filepath.Join(opts.Out, "root/modules/ROOT/pages/reagents.adoc"))
	if !strings.Contains(cat, "xref:reagent-redis:ROOT:index.adoc[Redis]") {
		t.Errorf("catalogue lacks redis:\n%s", cat)
	}
	if r := copyFile(t, filepath.Join(opts.Out, "reagents.txt")); !strings.Contains(r, "reagent-redis") || strings.Contains(r, "adept") {
		t.Errorf("reagents.txt = %q", r)
	}
	// Second run must succeed on an existing build dir.
	if err := runGenerate(context.Background(), opts, &log); err != nil {
		t.Fatalf("rerun: %v", err)
	}
}

func TestGenerate_SystemComponentMissingDocs(t *testing.T) {
	opts := fixture(t, false)
	err := runGenerate(context.Background(), opts, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "adept") {
		t.Fatalf("want error naming adept, got %v", err)
	}
}

func TestGenerate_ReagentBrokenDocs(t *testing.T) {
	opts := fixture(t, true)
	opts.Locals["reagent-broken"] = gitRepo(t, map[string]string{"docs/antora.yml": "name: reagent-broken\nversion: master\n", "Chart.yaml": "apiVersion: v2\nname: broken\nversion: 0.0.1\n", "values.yaml": "{}\n"})
	var log bytes.Buffer
	if err := runGenerate(context.Background(), opts, &log); err != nil {
		t.Fatalf("reagent failure must not fail the build: %v", err)
	}
	if !strings.Contains(log.String(), "WARN reagent-broken:") {
		t.Errorf("no warning logged:\n%s", log.String())
	}
	cat := copyFile(t, filepath.Join(opts.Out, "root/modules/ROOT/pages/reagents.adoc"))
	if !strings.Contains(cat, "== Skipped") || !strings.Contains(cat, "reagent-broken") {
		t.Errorf("broken reagent not in Skipped:\n%s", cat)
	}
}

func TestGenerate_SiteURL(t *testing.T) {
	opts := fixture(t, true)
	opts.SiteURL = "https://docs.helmetica.io/pr-preview/pr-7"
	if err := runGenerate(context.Background(), opts, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	pb := copyFile(t, filepath.Join(opts.Out, "playbook.yml"))
	if !strings.Contains(pb, "url: https://docs.helmetica.io/pr-preview/pr-7") || !strings.Contains(pb, "robots: disallow") {
		t.Errorf("playbook:\n%s", pb)
	}
}

func TestGenerate_ComponentDescriptionsFromGitHub(t *testing.T) {
	opts := fixture(t, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/helmetica-framework/repos" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[{"name":"adept","description":"Runs actions on instances","default_branch":"main"}]`))
	}))
	defer srv.Close()
	opts.NoDiscover = false
	opts.Client = discover.Client{HTTP: srv.Client(), APIBase: srv.URL, RawBase: srv.URL}
	if err := runGenerate(context.Background(), opts, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	c := copyFile(t, filepath.Join(opts.Out, "root/modules/ROOT/partials/components.adoc"))
	if !strings.Contains(c, "xref:adept::index.adoc[Adept]: Runs actions on instances") {
		t.Errorf("components partial:\n%s", c)
	}
}
