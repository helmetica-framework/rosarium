package site

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/helmetica-framework/rosarium/pkg/discover"
)

// ReagentTopic is hidden from the catalogue: every listed repo carries it.
var ReagentTopic = "helmetica-reagent"

// WriteCatalogue writes the reagent catalogue page (hub-style table).
func WriteCatalogue(path string, reagents []Source, skipped []discover.Skipped) error {
	sorted := slices.Clone(reagents)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Title < sorted[j].Title })

	var b strings.Builder
	b.WriteString("= Reagent catalogue\n" + generatedHeader + "\n")
	b.WriteString("Every service you can order on a Helmetica platform.\n\n")
	if len(sorted) == 0 {
		b.WriteString("No reagents are published yet.\n")
	} else {
		b.WriteString("[cols=\"2,4,2,1\",options=\"header\",stripes=even]\n|===\n|Reagent |Description |Upstream |Version\n")
	}
	for _, r := range sorted {
		desc := strings.ReplaceAll(r.Repo.Description, "{", `\{`)
		if desc == "" {
			desc = "—"
		}
		var topics []string
		for _, t := range r.Repo.Topics {
			if t != ReagentTopic {
				topics = append(topics, "`"+t+"`")
			}
		}
		if len(topics) > 0 {
			desc += " +\n" + strings.Join(topics, " ")
		}
		upstream, version := "—", "—"
		if r.Reagent != nil {
			if r.Reagent.PrimaMateria != "" {
				upstream = strings.TrimSpace(r.Reagent.PrimaMateria + " " + r.Reagent.PrimaMateriaVersion)
			}
			if r.Reagent.ChartVersion != "" {
				version = r.Reagent.ChartVersion
			}
		}
		fmt.Fprintf(&b, "\n|xref:%s:ROOT:index.adoc[%s] +\n_%s[GitHub ⇗]_\n|%s\n|%s\n|%s\n",
			r.Name, r.Title, r.Repo.HTMLURL, cell(desc), cell(upstream), cell(version))
	}
	if len(sorted) > 0 {
		b.WriteString("|===\n")
	}

	if len(skipped) > 0 {
		b.WriteString("\n== Skipped\n\nThese repos are tagged as reagents but their docs could not be rendered.\n\n")
		for _, s := range skipped {
			fmt.Fprintf(&b, "* %s[%s]: %s\n", s.Repo.HTMLURL, s.Repo.Name, plain(s.Reason))
		}
	}
	return writeFile(path, b.String())
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// plain makes arbitrary text (error messages, descriptions) safe as one
// line of AsciiDoc prose: no attribute references, no macro brackets.
func plain(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.NewReplacer("{", `\{`, "[", `\[`).Replace(s)
}
