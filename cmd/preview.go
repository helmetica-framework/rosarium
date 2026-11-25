package cmd

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/helmetica-framework/rosarium/pkg/config"
	"github.com/helmetica-framework/rosarium/pkg/site"
)

const (
	// renovate: datasource=docker depName=ghcr.io/vshn/antora
	antoraImage = "ghcr.io/vshn/antora:3.1.14.1"
	// renovate: datasource=npm depName=antora
	antoraNPMVersion = "3.1.14"
)

type runner func(ctx context.Context, dir string, args ...string) error

// RootDocs holds rosarium's docs/ tree (set by main via go:embed).
var RootDocs fs.FS

type previewOptions struct {
	RootDocs fs.FS // fs with a docs/ directory: the ROOT component
	RepoDir  string
	Kind     config.Kind // empty: detect
	Out      string
	Serve    string // listen address, empty: don't serve
	Run      runner // runs Antora; args are passed to antora
}

func init() {
	o := previewOptions{}
	c := &cobra.Command{
		Use:   "preview <repo-dir>",
		Short: "Build the docs of one component repo, as they will look on docs.helmetica.io",
		Long: "Builds a one-component site from <repo-dir>/docs plus its generated reference pages.\n" +
			"Fails on any Antora warning, so it doubles as the docs check in CI.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			o.RepoDir = abs
			if o.Out == "" {
				if o.Out, err = os.MkdirTemp("", "rosarium-preview-"); err != nil {
					return err
				}
			}
			if o.Out, err = filepath.Abs(o.Out); err != nil {
				return err
			}
			o.Run = antoraRunner(o.RepoDir)
			o.RootDocs = RootDocs
			return runPreview(cmd.Context(), o, cmd.ErrOrStderr())
		},
	}
	c.Flags().StringVar(&o.Out, "out", "", "build directory (default: a new temp dir)")
	c.Flags().StringVar((*string)(&o.Kind), "kind", "", "operator, tool, chart or reagent (default: detect from Chart.yaml / config/crd/bases)")
	c.Flags().StringVar(&o.Serve, "serve", "", "serve the result on this address, e.g. :2020")
	rootCmd.AddCommand(c)
}

func runPreview(ctx context.Context, o previewOptions, log io.Writer) error {
	kind := o.Kind
	detected := kind == ""
	if detected {
		kind = config.KindTool
		if _, err := os.Stat(filepath.Join(o.RepoDir, "Chart.yaml")); err == nil {
			kind = config.KindReagent
		} else if _, err := os.Stat(filepath.Join(o.RepoDir, "config", "crd", "bases")); err == nil {
			kind = config.KindOperator
		}
	}
	s, err := component(o.RepoDir, kind)
	if err != nil && kind == config.KindReagent && detected {
		// A library or starter chart (azoth, ferment) is not a reagent.
		fmt.Fprintf(log, "WARN not rendering Parameters: %v\n", err)
		if s, err = component(o.RepoDir, config.KindTool); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	s.Group, s.Branch = config.GroupReagents, "main"
	if kind == config.KindOperator {
		s.Group = config.GroupOperators
	}
	s.Repo.Name = filepath.Base(o.RepoDir)

	for _, d := range []string{"root", "ui", "generated", "stubs"} {
		if err := os.RemoveAll(filepath.Join(o.Out, d)); err != nil {
			return err
		}
	}
	// ROOT is the real framework docs, with an empty catalogue.
	if err := os.CopyFS(filepath.Join(o.Out, "root"), must(fs.Sub(o.RootDocs, "docs"))); err != nil {
		return fmt.Errorf("writing framework pages: %w", err)
	}
	if err := site.WriteCatalogue(filepath.Join(o.Out, "root", "modules", "ROOT", "pages", "reagents.adoc"), nil, nil); err != nil {
		return err
	}
	startPaths := []string{"root"}
	dir, err := site.WriteOverlay(o.Out, s)
	if err != nil {
		return err
	}
	if dir != "" {
		startPaths = append(startPaths, filepath.ToSlash(must(filepath.Rel(o.Out, dir))))
	}
	// Index-only stubs of the listed components, so xrefs to them resolve.
	if raw, err := fs.ReadFile(o.RootDocs, "components.yaml"); err == nil {
		var cfg config.Config
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("embedded components.yaml: %w", err)
		}
		for _, c := range cfg.Components {
			if c.Repo == s.Name {
				continue
			}
			files := map[string]string{
				"antora.yml":                    fmt.Sprintf("name: %s\nversion: ~\n", c.Repo),
				"modules/ROOT/pages/index.adoc": fmt.Sprintf("= %s\n\nPreview stub: the real page is on https://docs.helmetica.io/%s/.\n", c.Repo, c.Repo),
			}
			for p, content := range files {
				target := filepath.Join(o.Out, "stubs", c.Repo, p)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
					return err
				}
			}
			startPaths = append(startPaths, "stubs/"+c.Repo)
		}
	}
	root := site.Source{Name: "ROOT", Title: "Preview", Group: config.GroupFramework}
	if err := site.WriteSidebar(filepath.Join(o.Out, "ui", "partials", "nav-explore.hbs"), []site.Source{root, s}); err != nil {
		return err
	}
	if err := initBuildRepo(ctx, o.Out); err != nil {
		return err
	}
	playbook := filepath.Join(o.Out, "playbook.yml")
	if err := site.WritePlaybook(playbook, site.PlaybookInput{
		Sources:    []site.Source{s},
		BuildRepo:  o.Out,
		StartPaths: startPaths,
		UIDir:      filepath.Join(o.Out, "ui"),
		OutputDir:  filepath.Join(o.Out, "_public"),
	}); err != nil {
		return err
	}
	if err := o.Run(ctx, o.Out, "--cache-dir="+filepath.Join(o.Out, ".cache"), "--log-failure-level=warn", playbook); err != nil {
		return fmt.Errorf("antora: %w", err)
	}
	public := filepath.Join(o.Out, "_public")
	fmt.Fprintf(log, "Docs built: %s/%s/index.html\n", public, s.Name)
	if o.Serve == "" {
		return nil
	}
	fmt.Fprintf(log, "Serving on http://localhost%s/%s/ (Ctrl-C to stop)\n", o.Serve, s.Name)
	srv := &http.Server{Addr: o.Serve, Handler: http.FileServer(http.Dir(public))}
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func detectEngine() string {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if _, err := exec.LookPath("podman"); err == nil {
			return "podman"
		}
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	return "none"
}

// antoraCommand returns the command running Antora: in the vshn/antora
// image when a container engine exists (out and repo mounted at their own
// paths, so the playbook's absolute paths stay valid), else via npx.
func antoraCommand(engine, out, repo string, antoraArgs ...string) (string, []string) {
	if engine == "none" {
		return "npx", append([]string{"--yes", "-p", "antora@" + antoraNPMVersion, "-p", "asciidoctor-kroki", "antora"}, antoraArgs...)
	}
	args := []string{"run", "--rm", "--volume", out + ":" + out, "--volume", repo + ":" + repo, "--workdir", out}
	if engine == "podman" {
		args = append(args, "--userns=keep-id")
	} else {
		args = append(args, "--user", fmt.Sprintf("%d", os.Getuid()))
	}
	return engine, append(append(args, antoraImage), antoraArgs...)
}

func antoraRunner(repo string) runner {
	return func(ctx context.Context, dir string, args ...string) error {
		name, cargs := antoraCommand(detectEngine(), dir, repo, args...)
		c := exec.CommandContext(ctx, name, cargs...)
		c.Dir, c.Stdout, c.Stderr = dir, os.Stderr, os.Stderr
		return c.Run()
	}
}
