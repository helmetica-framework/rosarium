package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
)

// ReagentsFile lists the reagents of a build, one per line; written by generate.
const ReagentsFile = "reagents.txt"

func init() {
	var buildDir string
	c := &cobra.Command{
		Use:   "check-log <antora-json-log>",
		Short: "Fail on Antora warnings from system components; report reagent warnings without failing",
		Long: "Antora runs with --log-format=json --log-failure-level=fatal; this decides afterwards.\n" +
			"A problem in a reagent's docs must not keep the whole site from deploying.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			var reagents []string
			if raw, err := os.ReadFile(path.Join(buildDir, ReagentsFile)); err == nil {
				reagents = strings.Fields(string(raw))
			}
			if n := checkLog(f, reagents, cmd.ErrOrStderr()); n > 0 {
				return fmt.Errorf("%d Antora problem(s) outside reagent docs", n)
			}
			return nil
		},
	}
	c.Flags().StringVar(&buildDir, "build", "build", "build directory (for "+ReagentsFile+")")
	rootCmd.AddCommand(c)
}

type antoraEntry struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	File  struct {
		Path string `json:"path"`
	} `json:"file"`
	Source struct {
		URL      string `json:"url"`
		Worktree string `json:"worktree"`
	} `json:"source"`
}

// checkLog echoes the log and returns how many warn/error entries come from
// outside the reagents, plus every fatal entry.
func checkLog(r io.Reader, reagents []string, out io.Writer) int {
	failures := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		var e antoraEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			fmt.Fprintln(out, line)
			continue
		}
		switch e.Level {
		case "warn", "error":
		case "fatal":
			fmt.Fprintf(out, "FATAL %s\n", e.Msg)
			failures++
			continue
		default:
			continue
		}
		where := e.File.Path
		if where == "" {
			where = e.Source.URL
		}
		if r := reagentOf(e, reagents); r != "" {
			fmt.Fprintf(out, "WARN %s: %s (%s)\n", r, e.Msg, where)
			continue
		}
		fmt.Fprintf(out, "ERROR %s (%s)\n", e.Msg, where)
		failures++
	}
	return failures
}

func reagentOf(e antoraEntry, reagents []string) string {
	for _, r := range reagents {
		if strings.Contains(e.File.Path, "/generated/"+r+"/") || strings.HasPrefix(e.File.Path, "generated/"+r+"/") {
			return r
		}
		for _, src := range []string{e.Source.URL, e.Source.Worktree} {
			if src != "" && strings.TrimSuffix(path.Base(strings.TrimRight(src, "/")), ".git") == r {
				return r
			}
		}
	}
	return ""
}
