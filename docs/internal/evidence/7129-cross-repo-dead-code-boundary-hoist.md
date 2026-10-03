# #7129 cross-repo dead-code repository-boundary evidence, returned once

## Problem

`find_cross_repo_dead_code` at default arguments returned
`mcp_response_over_budget` (761,090 bytes against 262,144) for one ops-qa
repository. In a 10-row page the `unknown` rows were about 13 KB each and
12,381 bytes of each was `consumer_evidence`: the same 20 repository-level
boundary items, byte-identical on every row.

`bucketCrossRepoDeadCodeResults` (`go/internal/query/codequery/deadcode/cross_repo.go`)
appended `consumers.Boundary`, filtered by the request's consumer selector and
the caller's grant, to every candidate that had no visible entity evidence and no
hidden-consumer count. That filter depends on the request, not the entity, so
every such row got the same list.

## Change

Response shaping only. No read, query or graph change.

- The boundary list is filtered once per request. It stays in the `visible`
  slice classification reads, so buckets, `needs_evidence_reasons` and
  `hidden_consumer_evidence_count` are unchanged.
- The row's output no longer carries it: a fallback row keeps
  `consumer_evidence: []` (array type preserved) and gets
  `consumer_evidence_source: "repository_boundary"`. Other rows get `"entity"`.
  Every row gets `consumer_evidence_count` (length of its own list).
- The response gains `data.boundary_consumer_evidence` and
  `data.boundary_consumer_evidence_count`, always present. The list is emitted
  only when at least one row got `repository_boundary`; otherwise it is `[]` and
  the count 0, so a response whose rows all have entity evidence carries no
  boundary bytes, as before the change.
- The hoisted list is itself unbounded (one item per incoming relationship that
  the selector and grant admit). The follow-up (`evidence_detail` handles) caps
  it at 50; this change does not.
- A row counts as a fallback row only when boundary items were visible to it. A
  row nothing backs (no boundary relationships at all, so it classifies dead) is
  `entity` with count 0.

Not part of this change: per-entity evidence still ships in full on its row. That
is the `evidence_detail` follow-up.

## No-Regression Evidence:

Metric: est2x, the serialized MCP tool result with both wire copies
(`estimateResponseBytes` in `go/internal/mcp/dispatch_budget.go`), against the
262,144-byte dispatch budget. Test:
`TestFindCrossRepoDeadCodeBoundaryOnlyRepositoryFitsDefaultBudget`
(`go/internal/mcp/dispatch_cross_repo_boundary_budget_test.go`), real
`dispatchTool` and real HTTP handler. Fixture: `deadCodeBudgetStore` with 400
candidates, no per-entity evidence, 20 fully populated incoming relationships
(about 600 bytes per item as a `consumer_evidence` item), default MCP arguments
(limit 25).

| Run | est2x | vs budget |
|---|---|---|
| HEAD (6d0c1d81d) | 824,864 (`mcp_response_over_budget`) | 3.15x over |
| With the hoist | 187,776 | 71.6% of budget, 74,368 bytes headroom |
| Hoist reverted to the per-row append (seeded violation) | 855,726 (`mcp_response_over_budget`) | the test fails |

Emit-only-when-used: `TestCrossRepoDeadCodeBoundaryListIsOmittedWhenNoRowUsedIt`
(20 boundary relationships, entity evidence on every candidate) requires
`boundary_consumer_evidence` to be `[]` and its count 0. Seeded violation (always
emit the list): the test fails with 20 items. The fixture-1 est2x above is
unchanged by this rule (187,776), because every row there uses the fallback.

The seeded violation puts the per-row evidence back but keeps the new response keys (the hoisted list and the per-row `consumer_evidence_source` and `consumer_evidence_count`), so it carries the boundary evidence twice. That is why it is larger than HEAD, which has neither; the test fails in both because both are over budget.

The 824,864 figure is a synthetic fixture, not the live 761,090-byte reply; the
live number is NOT_CHECKED after this change (no ops-qa access in this
worktree).

Classification invariance:
`TestCrossRepoDeadCodeBoundaryHoistKeepsClassification`
(`cross_repo_boundary_classification_test.go`) pins bucket membership, reasons and
hidden counts for an unscoped caller, a consumer selector, a scoped caller with
an ungranted boundary consumer, and no boundary. It passes at HEAD before the
change and after it.

Golden corpus: the B-12 snapshot asserts
`live_by_consumer[].consumer_evidence[].citation`. A fallback row never reaches
`live_by_consumer` (boundary items are `needs_evidence`, which strong-live
classification skips), so those rows keep their entity citations and the snapshot
needs no repin. The live B-7 gate is NOT_RUN (it needs Docker and fixed ports).

## No-Observability-Change:

Response shaping on the handler's output rows only. No new span, metric or log;
the existing `SpanQueryDeadCodeInvestigation` span and the MCP dispatch
budget metrics (`response_bytes`, over-budget counter) already report the effect.
An operator sees the result as the `mcp_response_over_budget` rate for
`find_cross_repo_dead_code` falling.
