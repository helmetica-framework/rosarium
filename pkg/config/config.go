// Package config loads components.yaml, the explicit list of system components.
package config

import (
	"fmt"
	"os"
	"slices"

	"sigs.k8s.io/yaml"
)

// Group is a sidebar group on the site.
type Group string

const (
	GroupFramework Group = "Framework"
	GroupReagents  Group = "Reagents"
	GroupBuilding  Group = "Building reagents"
	GroupOperators Group = "Operators"
	GroupPlatform  Group = "Platform"
)

// GroupOrder is the order groups appear in the sidebar.
var GroupOrder = []Group{GroupFramework, GroupReagents, GroupBuilding, GroupOperators, GroupPlatform}

// Kind decides which reference page rosarium generates for a component.
type Kind string

const (
	KindOperator Kind = "operator" // API reference from config/crd/bases
	KindTool     Kind = "tool"
	KindChart    Kind = "chart"
	KindReagent  Kind = "reagent" // Parameters page from values.yaml
)

// Component is one explicitly listed system component.
type Component struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Group   Group  `json:"group"`
	Kind    Kind   `json:"kind"`
	Enabled bool   `json:"-"`

	EnabledRaw *bool `json:"enabled,omitempty"`
}

// Config is the content of components.yaml.
type Config struct {
	Org          string      `json:"org"`
	ReagentTopic string      `json:"reagentTopic"`
	Components   []Component `json:"components"`
}

// Load reads and validates components.yaml, applying defaults
// (branch "main", enabled true).
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.UnmarshalStrict(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for i := range c.Components {
		comp := &c.Components[i]
		if comp.Repo == "" {
			return Config{}, fmt.Errorf("%s: component %d has no repo", path, i)
		}
		if seen[comp.Repo] {
			return Config{}, fmt.Errorf("%s: duplicate repo %q", path, comp.Repo)
		}
		seen[comp.Repo] = true
		if comp.Branch == "" {
			comp.Branch = "main"
		}
		comp.Enabled = comp.EnabledRaw == nil || *comp.EnabledRaw
		if !slices.Contains(GroupOrder, comp.Group) {
			return Config{}, fmt.Errorf("%s: %s: unknown group %q", path, comp.Repo, comp.Group)
		}
		if comp.Group == GroupReagents {
			return Config{}, fmt.Errorf("%s: %s: group %q is reserved for discovered reagents", path, comp.Repo, comp.Group)
		}
		switch comp.Kind {
		case KindOperator, KindTool, KindChart, KindReagent:
		default:
			return Config{}, fmt.Errorf("%s: %s: unknown kind %q", path, comp.Repo, comp.Kind)
		}
	}
	return c, nil
}
