// Package source fetches component repos with git.
package source

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

func git(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

// Fetch makes dir a shallow checkout of branch from url, cloning on first
// use and updating in place afterwards.
func Fetch(ctx context.Context, url, branch, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		return git(ctx, filepath.Dir(dir), "clone", "-q", "--depth", "1", "--branch", branch, url, dir)
	}
	if err := git(ctx, dir, "fetch", "-q", "--depth", "1", url, branch); err != nil {
		return err
	}
	return git(ctx, dir, "reset", "-q", "--hard", "FETCH_HEAD")
}
