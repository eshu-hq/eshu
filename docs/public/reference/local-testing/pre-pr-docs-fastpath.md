# Pre-PR Documentation Fast Path

For a diff that is provably documentation/specs-only, `make pre-pr` skips four
lanes (#5721):

- the whole-module `go build` and `go vet`
- the whole-module `gofumpt` and `golangci-lint`
- the changed-package `go test` lane
- the race lane

Those four are the long part of a `make pre-pr` run, and on a docs diff they
have nothing to inspect. Skipping them also avoids the failure mode where they
fail on unrelated packages hitting per-test timeouts under concurrent-worktree
CPU load — a red gate with nothing to do with the diff, which had been forcing a
manual override on otherwise-clean docs PRs.

Everything else in `make pre-pr` still runs. The fast path changes which lanes
run, not whether the gate blocks.

## Classifier

The allowlist and the base resolver live in
`scripts/lib/pre-pr-docs-fastpath.sh`, table-tested by
`scripts/lib/test-pre-pr-docs-fastpath.sh`. The wiring between them and the gate
— reading the changed paths, deciding whether that list can be believed,
printing the banner — lives in `scripts/lib/pre-pr-lane.sh`.

The classifier fails closed twice over, because the allowlist and the input it is
fed can each be wrong on their own.

### The path allowlist

A path is fast-path-safe only when it matches one of these:

| Pattern | Note |
| --- | --- |
| `docs/**` | the docs tree, excluding `*.go` / `go.mod` / `go.sum` (see below) |
| a root-level `*.md` | `README.md`, `CLAUDE.md`, `AGENTS.md`, … — root-anchored, so a nested `README.md` under `go/` does not qualify |
| `specs/capability-matrix.v1.yaml` | the exact file, not a `capability-matrix`-prefixed sibling |
| `specs/capability-matrix/**` | matrix rows |
| `go/internal/capabilitycatalog/data/*.generated.json` | **one directory level only** — a nested `data/sub/x.generated.json` takes the full lane |

Anything else takes the FULL lane, including every other `specs/*.yaml`, every
`go/**/*.go` (a generated file such as `openapi*.go` included), `go.mod`,
`go.sum`, `Makefile`, `Dockerfile*`, anything under `scripts/`,
`.github/workflows/**`, and any path the classifier does not recognize at all.

Three rules narrow the allowlist further:

- **`*.go`, `go.mod`, and `go.sum` are never fast-path-safe, in any directory.**
  `docs/**` is a prefix match, so without this a `docs/examples/main.go` would
  ride the fast path and skip the build it can break. There are no Go files
  under `docs/` today — `rg --files docs -g '*.go'` returns 0 — and the rule
  keeps it that way without relying on nobody ever adding one.
- **An allowlisted path that no longer exists takes the FULL lane.** `git diff
  --name-only` prints a deleted path exactly like a modified one. That matters
  most for the generated catalog JSON: `go/internal/capabilitycatalog/load.go`
  and `surfaces_load.go` `go:embed` it by pattern, so editing its content cannot
  break `go build` but deleting or renaming it fails the build with
  `pattern …: no matching files found`.
- **An empty path argument is treated as unsafe**, not skipped.

### The input has to be trustworthy first

Every allowlist guarantee is conditional on `make pre-pr` handing over the real
changed-path set. Two conditions are checked before the allowlist is consulted at
all, and either one forces FULL:

- **The base must be a resolved `origin/main` that shares a merge base with
  HEAD.** When `origin/main` does not resolve, `pre-pr.sh` falls back to
  `HEAD~1`. That base is fine for picking which packages to test and useless for
  deciding to skip the Go lanes: on a multi-commit branch it shows only the last
  commit, so a branch whose final commit happens to be docs-only would classify
  fast while its real diff touches Go.
- **Every `git diff --name-only` that built the list must have exited 0.** On a
  shallow clone whose fetched `origin/main` does not reach the branch,
  `git diff origin/main...HEAD` exits 128 with `fatal: no merge base` and prints
  nothing on stdout. An empty list from a failed command is an absence of
  information, not an observation that nothing changed, so the two are kept
  apart: a *successful* empty diff is fast, a *failed* one is full.

`make pre-pr` prints which lane it selected. On the FULL lane it prints either
the changed paths that triggered it or the reason the path list could not be
believed.

### The classifier checks itself before it is trusted

`make pre-pr` runs `scripts/lib/test-pre-pr-docs-fastpath.sh` on every run,
before it classifies. Nothing downstream re-checks a FAST verdict — the per-SHA
stamp is written and `scripts/dev/prepr-stamp-verify.sh` lets the push through —
so the table suite is the only thing watching this decision. It is hermetic (no
Go toolchain, no network) and takes about a second. A failing self-check fails
the run and forces the FULL lane.

## What still runs on the fast path

Derive this list rather than trusting a copy of it. Selection is data-driven
(`specs/ci-gates.v1.yaml`), so it changes when the registry changes:

```bash
printf '%s\n' docs/public/reference/local-testing.md README.md \
  | go -C go run ./cmd/ci-gates select --registry ../specs/ci-gates.v1.yaml \
      --tier pre-pr --category exactness,telemetry,hygiene,docs --paths-from -
```

For a pure docs diff that currently selects:

```text
no-ai-attribution
capability-inventory-docs
docs-catalog-metadata
docs-prose-quality
docs-contradiction
docs-refs
measurement-citations
docs-build-changed
```

Plus the path-triggered live lane, which is a no-op for a docs diff — none of
its triggers are documentation paths.

Two gates people expect here and do not get: `remote-validation-artifacts` and
`capability-inventory` (verify) do **not** select for a pure docs diff. They do
select for a `specs/capability-matrix**` change, which is also fast-path-safe —
so the selected set depends on which kind of fast-path input you have, not just
on "it was fast".

The 500-line file cap and the package-docs gate run on every `make pre-pr`, fast
lane included, but both are no-ops for a docs-only diff: each filters the changed
set to `^go/.*\.go$` first and prints `no changed Go files — skipping file cap`.
Neither one caps the length of a Markdown page. 34 files under `docs/` are over
500 lines today.

### A `go/` fast path skips less than a `docs/` one

`go/internal/capabilitycatalog/data/*.generated.json` is fast-path-safe and also
lives under `go/**`, which several registry gates trigger on. Selecting for that
one path returns `go-fmt`, `go-lint`, `go-vet`, `go-file-cap` and `package-docs`
among others — so for that input, `gofumpt`, `golangci-lint` and `go vet` still
run, just as registry gates rather than as the whole-module lanes. Only `go
build`, `go test` and the race lane genuinely skip. `make pre-pr` says so in the
lane banner when any fast-path path is under `go/`.

## Relationship to CI's own docs-only skip

The two definitions **overlap; neither contains the other.** CI's docs-only PR
skip, described under
[CI workflow shape](../local-testing.md#ci-workflow-shape), covers `docs/**`, a
root-level `*.md`, `mkdocs.yml`, and `.agents/**`. This local classifier:

- **adds** `specs/capability-matrix.v1.yaml`, `specs/capability-matrix/**`, and
  `go/internal/capabilitycatalog/data/*.generated.json`
- **omits** `.agents/**`, and a root-level `mkdocs.yml` — this repo keeps its
  nav file at `docs/mkdocs.yml`, which `docs/**` already covers

They serve different gates with different blast radii, so they are allowed to
differ — but a change to either one is not automatically safe for the other.
