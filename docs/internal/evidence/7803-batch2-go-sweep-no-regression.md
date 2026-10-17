# Batch 2 environment-token sweep: no-regression note (#7803)

Batch 2 extends the #7803 sweep to the remaining Go and docs census hits:
environment names in comments and prose become neutral QA wording, four test
identifiers take neutral forms, and the fixture scrubber promotes to the whole
`go/internal/query` tree. No statement, predicate, bound, default, key, or
control-flow line changes outside test fixtures.

## What changed

- Comments and prose in `go/cmd`, shared packages, `go/internal/query`,
  collector, reducer, storage, and docs. Comment text only.
- Four registry `Description` strings in `go/internal/envregistry/entries.go`.
  They render into the generated reference doc and registry output. Keys,
  types, and defaults are unchanged. The reference doc was regenerated with
  `scripts/generate-env-registry-doc.sh`.
- Two `query-source-coverage.yaml` sharded digests refreshed for function
  bodies that changed only in comments. The discovery recomputation matches.
- Test-only renames asserted on both sides of each assertion: one test-local
  identifier, three scale-case names, one scale-test symbol with its readiness
  inventory and evidence-note touchpoints, and synthetic owner, namespace, and
  machine-path fixtures the promoted scrubber target newly covers.
- Scrubber gate, mirror, and CI triggers promoted from two file targets to
  the whole `go/internal/query` tree. The 7 pinned migration files and the
  append-only measurements ledger are untouched.

## No-Regression Evidence:

Baseline is the branch point `e7675d9ee`, which is also the merge base with
`origin/main`. Input shape of the three sweep commits (`43816059e`, `514f63525`, `f2b390a1f`)
is 117 files, 206 insertions, 215 deletions of comments, prose, and symmetric
test fixtures. The generated doc regen, this note, and the review fixes ride
on top and are enumerated above, never counted here, so this paragraph does
not chase its own diff. After measurement is the unit
proof battery on the local Go toolchain `go1.26.6 linux/arm64`, with no live
backend: `go build ./...` exits 0, and every touched package suite passes
with `-count=1`, including `go/internal/query/...`, `go/internal/queryplan`,
`go/internal/collector/...`, `go/internal/reducer/...`,
`go/internal/runtime/...`, `go/internal/storage/...`, `go/internal/mcp`,
`go/internal/envregistry`, and the touched `go/cmd` trees. The
`query-methodology-static` gate passes on the refreshed digests, the
identifier gate passes, the fixture scrubber gate and its RED/GREEN mirror
pass, and the agent-hygiene mirror passes 217 to 0. Terminal counts match the
base: no suite changed its pass set, and the only tests renamed are the
symmetric scale fixtures above. No benchmark delta is claimed because no
executed line outside test fixtures changed.

## Observability

No-Observability-Change: the diff adds no span, metric, or log line and
changes no emitted string outside one test diagnostic. An operator reading
dashboards at 3 AM sees identical telemetry before and after.
