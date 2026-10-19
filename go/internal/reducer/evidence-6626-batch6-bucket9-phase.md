# Evidence: retire phase alias block (#6626 batch 6, bucket 9)

## What changed

`go/internal/reducer/graph_projection_phase.go` (10 type aliases + 20
const aliases, all `=` into `go/internal/reducer/gpphase`) is deleted,
and every caller now spells the canonical `gpphase.*` name. One PR for
the whole bucket per arbiter ruling (consts travel with their types; a
five-slice split would re-touch ~186 files through mixed-spelling
intermediate states).

Pre-census on `origin/main a3346db78e` (exact commands; bare and
qualified counted separately — earlier prefix-conflated brief figures
corrected here per the derivable-figures rule):

- Qualified, `git grep -c "reducer\.NAME\b" -- '*.go'` per name, 30
  names: 10 types
  (`GraphProjectionKeyspace` 16 files/33 lines, `GraphProjectionPhase`
  10/22, `GraphProjectionPhaseKey` 16/61, `GraphProjectionPhaseState`
  16/42, `GraphProjectionReadinessLookup` 2/3,
  `GraphProjectionReadinessPrefetch` 2/2, `GraphProjectionPhasePublisher`
  8/8, `EndpointPresenceRow` 2/12, `EndpointPresenceWriter` 1/3,
  `EndpointPresenceLookup` 1/3) plus 20 consts (13 keyspace, e.g.
  `...CodeEntitiesUID` 17/64, `...ServiceUID` 14/51; 7 phase, e.g.
  `...CanonicalNodesCommitted` 19/72; three keyspace consts with zero
  callers: `RepoWorkloadPresence`, `SecurityGroupEndpointUID`,
  `SecurityGroupRuleUID`). Union: 57 files. (`-c` counts matching lines;
  one line can carry two hits, so line sums trail hit sums by a hair.)
- Bare in-package-reducer-root uses, per name over the 475 root files
  from `git ls-files | rg "^go/internal/reducer/[^/]*\.go$"` (~195
  matching lines across the 10 type names; consts bare mostly in the
  alias file itself plus a handful of root callers).
- Type-only qualified use is small; the consts carry the mass — which is
  why the consts travel with their types in this single diff.

Rewrite: scripted qualifier swap `reducer.X` → `gpphase.X` over code
spans only (strings/comments masked), explicit 30-name map, bare rule
with `X:` field-key and `.X` selector exclusions and longer-name
boundaries; `gpphase` import
added where missing (bare `".../reducer/gpphase"`, the existing
convention), 37 now-unused reducer-root imports dropped. Hand work,
all disclosed:

- 4 struct-field declarations whose field NAME mirrors the type
  (`defaults.go`, `defaults_handlers.go` ×2,
  `workload_materialization_handler.go`) keep their name with a repointed
  type — a qualified identifier is not a legal field name.
- 9 stale `reducer.X` references in `//` doc comments (dsl/tags `doc.go`,
  postgres `doc.go`, backfill fanin/memo/dupkey-test, phase-state,
  input-invalid-facts) repointed; bare-concept prose (`Result`-style
  field-path mentions, log labels) untouched.
- 8 living-doc lines across 5 AGENTS/README files plus 3 guide/reference
  lines (`tag-taxonomy.md`, postgres exported-surface + lifecycle
  guides) repointed; the `gpphase/AGENTS.md` dual-spelling paragraph
  rewritten for the single spelling. Line-neutral except the AGENTS
  paragraph (-1).
- 3 files at exactly 500 lines gained one import line: `//nolint:filelength`
  on the package clause with reason (split out of scope for this rename).
- 4 files missing the `gpphase` import (stdlib-only or import-less test
  helpers) fixed by hand.
- Guard test `TestBucket9PhaseRetired` in the new
  `compat_alias_retirement_phase_test.go` scans the whole reducer package
  directory for any re-declared alias (bucket 3–8 pattern). Audit before
  writing it: no competing declarations of the 30 names in the reducer
  root; subpackage-owned `Result` types live in other packages.
- The sibling Repair family (`GraphProjectionPhaseRepair`,
  `...RepairQueue`, `...RepairerConfig`, `...Repairer`,
  `...RepairsFromStates`, stanza in `compat_correlation.go`, canonical in
  `gpphase.PhaseRepair*` and `repair.*`) is a different bucket and stays,
  except one forced parameter repoint at `compat_correlation.go:438`
  (`states []GraphProjectionPhaseState` → `[]gpphase.PhaseState`) —
  required because the parameter type itself was a retired alias. Its 29
  qualified uses stay.
- No inventory re-pin: no enrolled replay-tier source carries the retired
  spellings (golden-corpus mirror passes unmodified). `git diff` over
  `gpphase/*.go` is empty — const values untouched.

Diff audit: filtering out qualifier swaps leaves only import blocks, the
deleted alias file, the hand fixes above, the guard, the docs, and this
note. Any other non-scripted hunk is a review finding.

Deliberate remainders: the Repair family (above); 20 `docs/` records
still quoting retired spellings in prose (18 `docs/internal`
design/evidence/architecture-review records plus ledger-style prose in
`docs/public/observability/telemetry-coverage.md` and
`docs/public/reference/azure-cloud-collector-contract.md`) — all
historical, immutable. 9 struct fields in the three touched structs
carry retired-family names (4 restored by this diff with name kept and
type repointed; the other 5 already correct: Repair-family names owned
by another bucket, short names, or pre-existing canonical spellings).
Every Go source and every living doc is clean.

## Why no regression

No-Regression Evidence: every alias is the identical canonical type or
constant, so each repoint is spelling-only; no signature, envelope, row,
ordering, or string-value change (storage/Cypher string values are
untouched — only Go identifiers).
Proof on this head: `go build ./...` exit 0; `go vet ./...` exit 0
(includes test files); gofmt clean over every touched file (one
untouched pre-existing gofmt-dirty file left alone);
`git diff --check` clean; `go test -count=1` green over all 11 touched
package trees; `-race` on reducer root, contract, and gpphase; tagged
file `repo_dependency_ifa_concurrency_live_test.go` vets clean under
`-tags ifarepodependencyproof`; guard RED (seeded re-declaration fails,
exit 1) / GREEN (clean tree passes, exit 0, with the bucket 3–8 guards);
`scripts/test-verify-golden-corpus-gate.sh` exit 0 (6 PASS, 0 FAIL);
`ci-gates validate --drift` pass; `test-verify-telemetry-coverage.sh`
49/49. Zero residual for all 10 type names + 20 const names by `git
grep` (anchored `.gitignore` can hide tracked files from `rg`); only
Repair-family hits remain, out of scope by design.

Local toolchain note: the repo pins go 1.26.9 while the host mise `go` is
1.27.2, whose build-cache export data the pinned toolchain cannot read,
so package-loading tests fail under the host driver. All Go proof above
ran with the 1.26.6 binary (auto-switches to cached 1.26.9), `CC=clang`,
and `GOROOT`/`GOBIN` unset with go1.26.6 first on `PATH`.

## Observability

No-Observability-Change: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob. Phase
plumbing keeps reporting through the existing spans and counters; only
the static type spelling at call sites changed.
