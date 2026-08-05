# Pre-PR Documentation Fast Path

For a diff that is provably documentation/specs-only, `make pre-pr` skips the
whole-module `go build`, `go vet`, `gofumpt`, `golangci-lint`, the
changed-package `go test` lane, and the race lane entirely (#5721), instead of
paying their full 15-25 minute cost on a diff that never touches Go. Skipping
those lanes also avoids the failure mode where they fail spuriously under
concurrent-worktree CPU load on packages the diff never touches, which had
been forcing a manual pre-pr override on otherwise-clean docs PRs.

## Classifier

The classifier (`scripts/lib/pre-pr-docs-fastpath.sh`, table-tested by
`scripts/lib/test-pre-pr-docs-fastpath.sh`) is an ALLOWLIST, fail-closed by
design: every changed path (against the same `origin/main` base every other
lane uses) takes the FULL lane unless it matches one of these recognized
documentation/specs patterns —

- `docs/**`
- a root-level `*.md` file (`README.md`, `CLAUDE.md`, `AGENTS.md`, ...) —
  root-anchored, so a nested `*.md` (e.g. a package `README.md` under `go/`)
  does not qualify
- `specs/capability-matrix.v1.yaml` and `specs/capability-matrix/**`
- `go/internal/capabilitycatalog/data/*.generated.json`

Any other changed path forces the FULL lane, including every other
`specs/*.yaml`, every `go/**/*.go` file (a generated file such as
`openapi*.go` included), `go.mod`, `go.sum`, `Makefile`, `Dockerfile*`,
anything under `scripts/`, `.github/workflows/**`, and any path the
classifier does not explicitly recognize. An unrecognized path always takes
the conservative FULL lane — the classifier never defaults to fast. `make
pre-pr` prints which lane it selected and, on the FULL lane, which changed
path(s) triggered it.

## What still runs on the fast path

The fast path still runs everything else the changed paths select: the
500-line file cap, package docs (a no-op with no `go/` change), the selected
exactness/telemetry/hygiene/docs gates — capability-inventory verify/docs,
the remote-validation artifact-existence gate, the docs build when a
docs/nav-visible page changed, `git diff --check`, no-AI-attribution, and
YAML lint among them — and the path-triggered live lane (a no-op for a
docs-only diff, since none of its triggers are documentation paths).

## Relationship to CI's own docs-only skip

This allowlist is deliberately narrower than CI's own "docs-only PR" skip
definition described under
[CI workflow shape](../local-testing.md#ci-workflow-shape) (which also covers
`.agents/**` and any root `*.md`, and is evaluated per-workflow rather than
per-path): the two serve different gates with different blast radii, and this
local classifier stays conservative on purpose rather than inheriting CI's
broader definition.
