// Package site writes the Antora inputs: generated overlays, the reagent
// catalogue, the grouped sidebar partial and the playbook.
package site

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/helmetica-framework/rosarium/pkg/config"
	"github.com/helmetica-framework/rosarium/pkg/discover"
	"github.com/helmetica-framework/rosarium/pkg/gen"
)

// Source is one Antora component rendered into the site.
type Source struct {
	Name, Title, URL, Branch, LocalDir string
	Nav                                []string // nav files of the repo's antora.yml
	Group                              config.Group
	Repo                               discover.Repo
	Reagent                            *gen.ReagentDoc
	CRDs                               []gen.CRDDoc
}

// Descriptor is the part of a component's antora.yml rosarium reads.
type Descriptor struct {
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Version any      `json:"version"`
	Nav     []string `json:"nav"`
}

// ReadAntoraYML reads the component descriptor in docsDir. Components must
// be versionless (version: ~).
func ReadAntoraYML(docsDir string) (Descriptor, error) {
	p := filepath.Join(docsDir, "antora.yml")
	raw, err := os.ReadFile(p)
	if err != nil {
		return Descriptor{}, err
	}
	var d Descriptor
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", p, err)
	}
	if d.Name == "" {
		return Descriptor{}, fmt.Errorf("%s: no name", p)
	}
	if d.Version != nil {
		return Descriptor{}, fmt.Errorf("%s: version is %v, rosarium needs versionless components (version: ~)", p, d.Version)
	}
	if d.Title == "" {
		d.Title = d.Name
	}
	return d, nil
}

func writeFile(p, content string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o644)
}
