package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/helmetica-framework/rosarium/pkg/config"
	"github.com/helmetica-framework/rosarium/pkg/discover"
	"github.com/helmetica-framework/rosarium/pkg/gen"
	"github.com/helmetica-framework/rosarium/pkg/site"
	"github.com/helmetica-framework/rosarium/pkg/source"
)

type generateOptions struct {
	Root       string            // rosarium checkout (docs/, ui/)
	Config     string            // components.yaml
	Out        string            // build dir
	CacheDir   string            // clones of remote repos
	Locals     map[string]string // repo name -> local checkout, instead of cloning
	NoDiscover bool
	SiteURL    string // empty: production
	Client     discover.Client
}

func init() {
	var locals []string
	o := generateOptions{}
	c := &cobra.Command{
		Use:   "generate",
		Short: "Fetch all sources and write the Antora playbook, generated pages and UI overrides",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Locals = map[string]string{}
			for _, l := range locals {
				name, dir, ok := strings.Cut(l, "=")
				if !ok {
					return fmt.Errorf("--local %q: want <repo>=<path>", l)
				}
				abs, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				o.Locals[name] = abs
			}
			o.Client.Token = os.Getenv("GITHUB_TOKEN")
			return runGenerate(cmd.Context(), o, cmd.ErrOrStderr())
		},
	}
	c.Flags().StringVar(&o.Out, "out", "build", "build directory")
	c.Flags().StringVar(&o.Config, "config", "components.yaml", "system component list")
	c.Flags().StringVar(&o.Root, "root", ".", "rosarium checkout holding docs/ and ui/")
	c.Flags().StringVar(&o.CacheDir, "cache-dir", ".cache/sources", "where remote repos are cloned")
	c.Flags().StringArrayVar(&locals, "local", nil, "use a local checkout: <repo>=<path> (repeatable); unknown repos are treated as reagents")
	c.Flags().BoolVar(&o.NoDiscover, "no-discover", false, "skip reagent discovery on GitHub")
	c.Flags().StringVar(&o.SiteURL, "site-url", "", "site URL (default "+site.SiteURL+"); any other URL, e.g. a PR preview, is marked robots: disallow")
	rootCmd.AddCommand(c)
}

func runGenerate(ctx context.Context, o generateOptions, log io.Writer) error {
	cfg, err := config.Load(o.Config)
	if err != nil {
		return err
	}
	if site.ReagentTopic = cfg.ReagentTopic; site.ReagentTopic == "" {
		site.ReagentTopic = "helmetica-reagent"
	}
	out, err := filepath.Abs(o.Out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	fetch := func(repo, branch string) (string, error) {
		if dir, ok := o.Locals[repo]; ok {
			return dir, nil
		}
		dir, err := filepath.Abs(filepath.Join(o.CacheDir, repo))
		if err != nil {
			return "", err
		}
		return dir, source.Fetch(ctx, fmt.Sprintf("https://github.com/%s/%s.git", cfg.Org, repo), branch, dir)
	}
	repoInfo := func(name string) discover.Repo {
		return discover.Repo{Name: name, DefaultBranch: "main", HTMLURL: fmt.Sprintf("https://github.com/%s/%s", cfg.Org, name)}
	}

	// System components: any failure fails the build.
	var sources []site.Source
	listed := map[string]bool{}
	for _, c := range cfg.Components {
		listed[c.Repo] = true
		if !c.Enabled {
			continue
		}
		dir, err := fetch(c.Repo, c.Branch)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Repo, err)
		}
		s, err := component(dir, c.Kind)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Repo, err)
		}
		s.Group, s.Branch, s.Repo = c.Group, c.Branch, repoInfo(c.Repo)
		sources = append(sources, s)
	}

	// Reagents: failures are skipped with a warning.
	var candidates []discover.Repo
	var skipped []discover.Skipped
	if !o.NoDiscover {
		found, sk, err := o.Client.Reagents(ctx, cfg.Org, cfg.ReagentTopic)
		if err != nil {
			return err
		}
		candidates, skipped = found, sk
		// A local checkout overrides the "no docs/antora.yml on GitHub" verdict.
		var keep []discover.Skipped
		for _, s := range skipped {
			if _, ok := o.Locals[s.Repo.Name]; ok {
				candidates = append(candidates, s.Repo)
			} else {
				keep = append(keep, s)
			}
		}
		skipped = keep
	}
	known := map[string]bool{}
	for _, r := range candidates {
		known[r.Name] = true
	}
	var localNames []string
	for name := range o.Locals {
		if !listed[name] && !known[name] {
			localNames = append(localNames, name)
		}
	}
	sort.Strings(localNames)
	for _, n := range localNames {
		candidates = append(candidates, repoInfo(n))
	}
	var reagents []site.Source
	for _, r := range candidates {
		branch := r.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		s, err := func() (site.Source, error) {
			dir, err := fetch(r.Name, branch)
			if err != nil {
				return site.Source{}, err
			}
			return component(dir, config.KindReagent)
		}()
		if err != nil {
			fmt.Fprintf(log, "WARN %s: %v\n", r.Name, err)
			skipped = append(skipped, discover.Skipped{Repo: r, Reason: err.Error()})
			continue
		}
		s.Group, s.Branch, s.Repo = config.GroupReagents, branch, r
		reagents = append(reagents, s)
	}
	sources = append(sources, reagents...)
	for _, s := range skipped {
		if s.Reason == "no docs/antora.yml" {
			fmt.Fprintf(log, "WARN %s: %s\n", s.Repo.Name, s.Reason)
		}
	}

	// Generated content, all below out.
	for _, d := range []string{"root", "ui", "generated"} {
		if err := os.RemoveAll(filepath.Join(out, d)); err != nil {
			return err
		}
	}
	if err := copyDir(filepath.Join(o.Root, "docs"), filepath.Join(out, "root")); err != nil {
		return fmt.Errorf("copying rosarium docs: %w", err)
	}
	if err := copyDir(filepath.Join(o.Root, "ui"), filepath.Join(out, "ui")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("copying rosarium ui: %w", err)
	}
	startPaths := []string{"root"}
	for _, s := range sources {
		dir, err := site.WriteOverlay(out, s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		if dir != "" {
			startPaths = append(startPaths, filepath.ToSlash(must(filepath.Rel(out, dir))))
		}
	}
	if err := site.WriteCatalogue(filepath.Join(out, "root", "modules", "ROOT", "pages", "reagents.adoc"), reagents, skipped); err != nil {
		return err
	}
	all := append([]site.Source{{Name: "ROOT", Title: "Helmetica Framework", Group: config.GroupFramework}}, sources...)
	if err := site.WriteSidebar(filepath.Join(out, "ui", "partials", "nav-explore.hbs"), all); err != nil {
		return err
	}
	// check-log treats Antora problems from these as warnings.
	var names []string
	for _, r := range reagents {
		names = append(names, r.Name)
		if r.Repo.Name != "" && r.Repo.Name != r.Name {
			names = append(names, r.Repo.Name)
		}
	}
	if err := os.WriteFile(filepath.Join(out, ReagentsFile), []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	if err := initBuildRepo(ctx, out); err != nil {
		return err
	}
	for i := range sources {
		// Antora resolves a relative URL against the playbook only with "./".
		if rel, err := filepath.Rel(out, sources[i].LocalDir); err == nil {
			sources[i].LocalDir = "./" + filepath.ToSlash(rel)
		}
	}
	return site.WritePlaybook(filepath.Join(out, "playbook.yml"), site.PlaybookInput{
		Sources:    sources,
		BuildRepo:  ".",
		StartPaths: startPaths,
		UIDir:      "./ui",
		OutputDir:  "./_public",
		SiteURL:    o.SiteURL,
	})
}

// component reads the docs descriptor and generated reference of one repo.
func component(dir string, kind config.Kind) (site.Source, error) {
	d, err := site.ReadAntoraYML(filepath.Join(dir, "docs"))
	if errors.Is(err, fs.ErrNotExist) {
		return site.Source{}, errors.New("no docs/antora.yml")
	}
	if err != nil {
		return site.Source{}, err
	}
	s := site.Source{Name: d.Name, Title: d.Title, Nav: d.Nav, LocalDir: dir}
	switch kind {
	case config.KindReagent:
		r, err := gen.Reagent(dir)
		if err != nil {
			return site.Source{}, err
		}
		s.Reagent = &r
	case config.KindOperator:
		if s.CRDs, err = gen.Operator(dir); err != nil {
			return site.Source{}, err
		}
	}
	return s, nil
}

// initBuildRepo makes out a git repo with one commit: Antora only reads
// content sources from git repos (HEAD uses the worktree).
func initBuildRepo(ctx context.Context, out string) error {
	if _, err := os.Stat(filepath.Join(out, ".git")); err == nil {
		return nil
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=rosarium", "-c", "user.email=rosarium@localhost", "commit", "-q", "--allow-empty", "-m", "rosarium build"},
	} {
		c := exec.CommandContext(ctx, "git", args...)
		c.Dir = out
		if b, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w\n%s", args, err, b)
		}
	}
	return nil
}

func copyDir(from, to string) error {
	if _, err := os.Stat(from); err != nil {
		return err
	}
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
