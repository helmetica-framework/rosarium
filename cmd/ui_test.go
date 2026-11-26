package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The UI bundle's stylesheet name carries a content hash that changes with
// every bundle release: no override may pin it.
func TestUIOverridesDoNotPinBundleAssets(t *testing.T) {
	hashed := regexp.MustCompile(`css/site-[0-9a-f]+\.css`)
	err := filepath.WalkDir("../ui", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if hashed.Match(b) {
			t.Errorf("%s pins a hashed bundle asset", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("../ui/partials/helmetica-styles.hbs"); err != nil {
		t.Error("helmetica-styles.hbs missing")
	}
}
