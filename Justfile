export GOEXPERIMENT := "jsonv2"

# renovate: datasource=github-releases depName=vshn/antora versioning=loose
antora_version := "3.1.14.1"
# renovate: datasource=npm depName=pagefind
pagefind_version := "1.5.2"

# Antora runs in the vshn/antora image when a container engine is available,
# otherwise straight from npm (node required).
engine := `if command -v podman >/dev/null 2>&1 && [ "${GITHUB_ACTIONS:-}" != "true" ]; then echo podman; elif command -v docker >/dev/null 2>&1; then echo docker; else echo none; fi`
engine_opts := if engine == "podman" { "--rm --userns=keep-id" } else { "--rm --user $(id -u)" }
antora_image := "ghcr.io/vshn/antora:" + antora_version
# renovate: datasource=npm depName=antora
antora_npm_version := "3.1.14"
# Not renovated: asciidoctor-kroki 1.0+ needs Asciidoctor.js 3, Antora 3.1 ships 2.
kroki_npm_version := "0.18.2"
antora_cmd := if engine == "none" { "npx --yes -p antora@" + antora_npm_version + " -p asciidoctor-kroki@" + kroki_npm_version + " antora" } else { engine + " run " + engine_opts + " --volume \"$PWD\":/antora --workdir /antora " + antora_image }
pagefind_cmd := if engine == "none" { "npx --yes pagefind@" + pagefind_version } else { engine + " run " + engine_opts + " --volume \"$PWD\":/antora --workdir /antora --env npm_config_cache=/antora/.cache/npm --entrypoint npx " + antora_image + " --yes pagefind@" + pagefind_version }

_default:
    @just --list

# Run the tests
test:
    go test ./... -race

# Discover sources and write build/ (playbook, generated pages, UI overrides)
generate *FLAGS:
    go run . generate --out build {{ FLAGS }}

# Run Antora over build/playbook.yml. Warnings from system components fail the
# build; warnings from reagent docs are reported but do not block the deploy.
antora:
    #!/usr/bin/env bash
    set -uo pipefail
    {{ antora_cmd }} --cache-dir=.cache/antora --log-format=json --log-failure-level=fatal build/playbook.yml > build/antora.log 2>&1
    status=$?
    go run . check-log --build build build/antora.log || exit 1
    exit $status

# Build the Pagefind search index
pagefind:
    {{ pagefind_cmd }} --site build/_public

# Full site build
build *FLAGS: (generate FLAGS) antora pagefind

# Serve build/_public on http://localhost:2020
preview:
    python3 -m http.server 2020 --directory build/_public

clean:
    rm -rf build .cache
