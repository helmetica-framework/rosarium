# rosarium

> *Rosarium philosophorum sive pretiosissimum donum Dei*: where all the helmetica knowledge is gathered.

rosarium builds [docs.helmetica.io](https://docs.helmetica.io) from the `docs/` folder of every Helmetica repo.
It is an [Antora](https://antora.org) site plus a small Go generator that collects the sources and writes the reference pages nobody should write by hand.

## How content gets in

* **System components** (operators, tools, charts) are listed in [`components.yaml`](components.yaml), each with a sidebar group and a kind. Operators (`kind: operator`) get an API reference generated from `config/crd/bases/*.yaml`.
* **Reagents** are discovered: every public, non-archived repo in the `helmetica-framework` org with the GitHub topic `helmetica-reagent` and a `docs/antora.yml`. Each gets a Parameters page generated from its `values.yaml` (via chrysopoeia's schema generator, so it lists exactly what end users can set) and a row in the catalogue.
* The framework-wide pages (landing page, concepts, architecture) live in [`docs/`](docs).

Every component must be versionless (`version: ~` in its `docs/antora.yml`).
A listed component that fails breaks the build. A reagent whose descriptor or chart fails is skipped and listed under "Skipped" on the catalogue; Antora problems in a reagent's pages are reported as warnings and do not block the deploy.

The site is rebuilt on every push and every hour, and published to the `gh-pages` branch.
Every pull request from a branch of this repo gets a preview at `https://docs.helmetica.io/pr-preview/pr-<N>/`, linked in a PR comment and removed when the PR closes.

## Building locally

```bash
just test       # unit tests
just build      # discover, generate, run Antora and Pagefind into build/_public
just preview    # serve build/_public on http://localhost:2020
```

Use a local checkout instead of the GitHub default branch (repeatable; unknown repos are treated as reagents):

```bash
just build --local reagent-redis=../reagent-redis
```

Antora runs in the `ghcr.io/vshn/antora` image when docker or podman is available, otherwise via `npx`.

## Previewing one component's docs

From any component repo:

```bash
go run github.com/helmetica-framework/rosarium@main preview . --serve :2020
```

This builds that repo's docs with its generated reference pages and fails on any Antora warning, which is why the component repos also run it in CI.

## One-time GitHub setup

1. Set the topic `helmetica-reagent` on every reagent repo.
2. Enable GitHub Pages for this repo from the `gh-pages` branch with custom domain `docs.helmetica.io`.
3. DNS: `docs.helmetica.io CNAME helmetica-framework.github.io`.
