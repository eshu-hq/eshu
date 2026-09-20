# #6695 collector repo package move

## Scope

Base: `origin/main` at `0633e11c3`.

This slice nests the repository readers under `go/internal/collector/repo/`:
`gitrepo` becomes `repo/git` (package `git`), and `discovery`, `submodule`,
`codeowners` move under `repo/` unchanged in content. Leaf subpackages are
de-compounded per naming rule 5 (`gitmodel` to `model` as the issue
mandates, plus `gitdocs` to `docs`, `gitcodeowners` to `codeowners`,
`gitsubmodule` to `submodule`, `gitobs` to `observability`,
`gitsvccatalog` to `service/catalog`, `gittfstate` to `tfstate`,
`workflowimage` to `workflow/image`) and directory-prefix file stutter is
dropped (`git_snapshot_*` to `snapshot_*`, and so on). No exported
identifiers are renamed and no production logic is edited.

The `repo` parent is a documentation-only namespace with a new
`doc.go`/`README.md`/`AGENTS.md` trio. Imports run one way
(`git -> leaf -> model`); no leaf imports `git`. Three leaf names
intentionally repeat a sibling parser package (`codeowners`, `submodule`,
`service/catalog`): each is the git-collector hook over that parser, and
the split is documented in `repo/git/README.md`.

Rebased onto `e741fa981` (#6862) mid-slice: that PR moved content
envelopes to `collector/git/content` and deleted
`git_content_fact_envelopes.go`. This slice takes #6862's package as the
truth (no resurrection) and merges its `fact_builder.go` changes with the
new import paths and qualifiers.

## Inventory and dependency edges

At the base, `gitrepo` held 65 non-test Go files plus the eight leaf
subpackages; `discovery`, `submodule`, and `codeowners` held 6, 7, and 6.
Every file moves 1:1 (plus renames); no file splits or merges. Outside
importers (`cmd/collector-git`, `cmd/ingester`, `cmd/bootstrap-index`,
`cli/localsupervisor`, `replay/parserfixture`, `query/repositoryartifacts`,
`storage/postgres` tests) take the mechanical import repoint only.

## Verification

- `go build ./...` and `go vet ./...` clean.
- Recursive `go test ./internal/collector/...` green, including golden,
  cassette, ratchet, and fingerprint suites; importer suites green.
- `go test -list '.*'` discovers 473 tests on both sides.
- Changed-package gates green: dirgate (ledger row re-derived for
  `repo/git`, 65 files), filecap, lint (35 packages, 0 issues), gofumpt.
- `scripts/verify-telemetry-coverage.sh` agrees; citations baseline
  regenerated via the sanctioned `-update`; docs strict build green.

Deliberately not run (orchestrator promotion gates): `make pre-pr`,
`make pre-pr-full`. Promotion runs `eshu-code-review` (self-review, READY,
P0/P1/P2-blocking 0), `review-attest` capture/verify, and `make pre-push`.

## No-Regression Evidence (#6695):

- Baseline: branch base `origin/main` at `0633e11c3`, measured in a
  throwaway worktree (`/tmp/gitrepo-base`, removed after the run) on the
  old paths `go/internal/collector/gitrepo/...`,
  `go/internal/collector/discovery`, `go/internal/collector/submodule`,
  `go/internal/collector/codeowners`.
- After: branch `feat/6695-gitrepo-leaf` at the leaf commits, new paths
  under `go/internal/collector/repo/`.
- Backend/version: no live backend exercised. Unit, golden, cassette, and
  ratchet tests only against in-repo fixtures; no NornicDB, Postgres, or
  Docker in these packages. Toolchain `go1.27.1 darwin/arm64` both sides,
  same machine, serial runs. Wall times are same-machine relative
  readings, not reference targets.
- Input shape: `go test -count=1` on the old paths (base) and the new
  paths (branch); `go test -list '.*'` for test discovery.
- Baseline measurement: `gitrepo` ok 10.390s, `gitdocs` ok 1.327s,
  `discovery` ok 1.112s, `submodule` ok 0.955s, `codeowners` ok 0.793s.
- After measurement: `git` ok 10.216s, `docs` ok 0.773s, `discovery` ok
  0.278s, `submodule` ok 0.917s, `codeowners` ok 0.424s.
- Terminal counts: 473/473 tests discovered on both sides, all green,
  zero failures.
- Query/concurrency proof: the move is a pure import-path relabeling, so
  the import graph is isomorphic and no new cycles or goroutines are
  possible; the added-line scan of the Go diff finds no Cypher/SQL
  keywords, no telemetry identifiers, and no new goroutine lines.
  `fact_builder.go` was diffed against #6862's version and shows only
  renames plus gofumpt reflow.
- Telemetry/log/status evidence: `go vet` clean on the whole module; the
  telemetry verifier agrees no new untracked stages.
- Why the change is safe: rename-only nest with mechanical importer
  repoints (compiler-checked by the whole-module build); the relocated
  tests pass in the baseline time band with identical test counts.

## No-Observability-Change (#6695):

- The `telemetry-coverage.md` rows for the git collector name the new
  `repo/git/*.go` paths with unchanged reasons; moved-file `:line`
  anchors were converted to file anchors per the citations gate's
  exact-bytes authority, matching prior leaves.
- `scripts/verify-telemetry-coverage.sh` agrees: no new untracked stages.
