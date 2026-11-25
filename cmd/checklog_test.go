package cmd

import (
	"bytes"
	"strings"
	"testing"
)

const antoraLog = `{"level":"info","msg":"starting"}
{"level":"error","msg":"target of xref not found: x.adoc","file":{"path":"docs/modules/ROOT/pages/index.adoc"},"source":{"url":"./../.cache/sources/reagent-redis","worktree":"/antora/.cache/sources/reagent-redis"}}
{"level":"warn","msg":"skipping reference to missing attribute: name","file":{"path":"/antora/build/generated/reagent-redis/modules/ROOT/pages/references/parameters.adoc"},"source":{"url":"."}}
{"level":"error","msg":"target of xref not found: y.adoc","file":{"path":"docs/modules/ROOT/pages/index.adoc"},"source":{"url":"./../.cache/sources/adept","worktree":"/antora/.cache/sources/adept"}}
not json at all
`

func TestCheckLog_ReagentProblemsAreWarnings(t *testing.T) {
	var out bytes.Buffer
	failures := checkLog(strings.NewReader(antoraLog), []string{"reagent-redis"}, &out)
	if failures != 1 {
		t.Errorf("want 1 failure (adept), got %d\n%s", failures, out.String())
	}
	o := out.String()
	for _, want := range []string{"WARN reagent-redis: target of xref not found: x.adoc", "WARN reagent-redis: skipping reference", "ERROR target of xref not found: y.adoc", "not json at all"} {
		if !strings.Contains(o, want) {
			t.Errorf("output missing %q:\n%s", want, o)
		}
	}
}

func TestCheckLog_FatalAlwaysFails(t *testing.T) {
	log := `{"level":"fatal","msg":"boom","source":{"url":"./../.cache/sources/reagent-redis"}}` + "\n"
	if n := checkLog(strings.NewReader(log), []string{"reagent-redis"}, &bytes.Buffer{}); n != 1 {
		t.Errorf("fatal must fail, got %d", n)
	}
}
