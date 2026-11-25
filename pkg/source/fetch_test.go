package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestFetch_CloneAndUpdate(t *testing.T) {
	src := gitRepo(t, map[string]string{"docs/antora.yml": "name: x\n"})
	dst := filepath.Join(t.TempDir(), "x")
	if err := Fetch(context.Background(), "file://"+src, "main", dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "docs/antora.yml")); err != nil {
		t.Fatal(err)
	}
	// second fetch updates in place
	os.WriteFile(filepath.Join(src, "new.txt"), []byte("n"), 0o644)
	exec.Command("git", "-C", src, "add", ".").Run()
	exec.Command("git", "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "2").Run()
	if err := Fetch(context.Background(), "file://"+src, "main", dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "new.txt")); err != nil {
		t.Fatal("update not applied:", err)
	}
}

func TestFetch_BadBranch(t *testing.T) {
	src := gitRepo(t, map[string]string{"a": "a"})
	if err := Fetch(context.Background(), "file://"+src, "nope", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("want error")
	}
}
