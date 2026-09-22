# Spine repoint: authz and capability forwarders onto querycontract (#6642 PR 1b)

PR 1b of the [query target tree](../design/6642-query-target-tree.md). Moves no
file and changes no query shape.

PR 1 ([#6977](https://github.com/eshu-hq/eshu/pull/6977)) repointed the envelope
pair, `requiredProfile` and `acceptsEnvelope`. It did not clear the rest of the
spine. Three unexported root symbols remained, each a one-line forwarder onto an
exported twin that already existed in `querycontract`:

| symbol | declared in | root non-test files naming it | all non-test files under `query/` | twin |
| --- | --- | ---: | ---: | --- |
| `capabilityUnsupported` | `handler.go` | 25 | 25 | `querycontract.CapabilityUnsupported` |
| `repositoryAccessFilterFromContext` | `repository_authz.go` | 29 | 30 | `querycontract.RepositoryAccessFilterFromContext` |
| `repositoryAccessFilter` (a type) | `repository_authz.go` | 20 | 24 | `querycontract.RepositoryAccessFilter` |

Counted on the base tree with `git ls-tree -r`, root meaning files directly in
`go/internal/query/`. An earlier draft of this table printed the recursive
counts under a "root files" heading, which overstated two of the three rows.

`repository_authz.go` carried a comment recording the deliberate decision to
keep them — "Go has no function aliases, so the ~185 call sites keep this
unexported name rather than being rewritten." #6642's no-retained-aliases rule
overturns that, because each of these names pins its callers to package `query`
and so blocks every family that later leaves root.

The exported `RepositoryAccessFilter` spelling stays. It is a deliberate
impact-seam alias named in exported forwarder signatures (#6060), not a
compat shim, and nothing in this issue retires it.

`startQueryHandlerSpan` is the fifth symbol in the plan's spine table and is
**not** touched here: the `tracing` -> `span` rename belongs to
[#6818](https://github.com/eshu-hq/eshu/issues/6818), whose PR has already
merged.

No-Regression Evidence: every changed call site resolves to the identical
function body the deleted forwarder already delegated to, so there is no runtime
delta to measure on any query path. The one signature change,
`relationshipEdgesCypher(entry, access querycontract.RepositoryAccessFilter)`,
swaps an alias for the type it already aliased; the Cypher string the function
returns is byte-identical. Backend go1.27 darwin/arm64. Measured on this branch
from `go/`: `go build -gcflags=-e ./internal/query/...` exit 0; `go vet
./internal/query/...` exit 0; `go test ./internal/query/... ./internal/queryplan/...
-count=1` exit 0, 54 packages ok. No Cypher statement text, anchor, index, batch
size, worker count, lease, or timeout changed.

No-Observability-Change: no span, metric, log key, or status field is added,
removed, or renamed. `handler_tracing.go` is untouched.

## Tests still run, and the build-tagged files do compile

The repoint touched 40 test files, so "it compiles" is not enough — a test can
survive compilation and stop being discovered. Test discovery is byte-identical
across the change: `go test ./internal/query/... -list '.*' -count=1` returns
the same **5181** names before and after, and `diff` of the two sorted lists is
empty.

Seven of the touched files sit behind `//go:build` tags, and **none of those
four tag names appears in any file under `.github/workflows`, `Makefile`, or
`scripts/`**. A default `go build/vet/test ./...` never compiles them, so the
green runs above did not cover these seven files. Verified separately:

| tag | `go vet -tags` | tests discovered (untagged baseline 2689) |
| --- | --- | ---: |
| `live_global_name_comparison` | clean | 2691 |
| `live_infra_scope_shape` | clean | 2693 |
| `live_nornicdb_answer_truth` | clean | 2690 |
| `live_nornicdb_language_imports_grant` | clean | 2696 |

Each tag raises the count above the baseline, so the tagged files are genuinely
compiled and their tests registered, not silently skipped.

That no workflow references these tags is a pre-existing gap, not one this
change introduces: those live tests run nowhere. Wiring four CI lanes is well
outside a spine repoint and needs an owner decision about the backends they
require, so it is recorded here rather than acted on.

## Queryplan digests re-pinned

Ten digests moved across eleven pinned lines — `(*InfraHandler).searchResources`
is pinned twice — in `query-source-coverage.yaml`, `hot-cypher.yaml` and
`grandfathered_non_hot.go`. Each was re-derived by iterating the binding tests
to a fixed point, never by editing a hash by hand.

`go/internal/query/codequery/metrics/edges.go` carries pre-existing gofumpt
drift and is deliberately absent from this diff, so its digest does not move.

## The digest surface spans two packages

`go test ./internal/queryplan/...` is not sufficient to prove the manifest is
consistent. `go/internal/query/queryplan_legacy_production_binding_test.go`
loads the same `testdata/hot-cypher.yaml` and binds `relationshipEdgesCypher`,
a symbol the `queryplan` package's own tests never check. This change left
`./internal/queryplan/...` fully green while `QP-RELATIONSHIPS-EDGES` was still
stale, and only the recursive `./internal/query/...` run caught it.

Any later PR in this series that touches a pinned symbol must run both
surfaces. Running one and reporting it as "the queryplan gate" is a false green.
