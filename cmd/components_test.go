package cmd

import (
	"testing"

	"github.com/helmetica-framework/rosarium/pkg/config"
)

func TestComponentsYAMLIsValid(t *testing.T) {
	c, err := config.Load("../components.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Components) != 13 {
		t.Errorf("want 13 system components (spec §4), got %d", len(c.Components))
	}
}
