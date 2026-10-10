# #7129 cross-repo dead-code per-entity evidence: `evidence_detail` handles

## Problem

The boundary hoist (see `7129-cross-repo-dead-code-boundary-hoist.md`) removed the
evidence every fallback row repeated. Per-entity evidence still ships in full on
each row. The SQL page behind `find_cross_repo_dead_code` can return up to 1,000
evidence items, about 616 bytes each as `consumer_evidence` objects, so a 25-row
page with 40 items per row is roughly 1.2 MB of evidence alone against a
262,144-byte dispatch budget. A per-item keyed handle object measured 190 bytes
per item (about 380 KB est2x for 1,000 items), so per-item handles do not fit;
grouping is what makes the cap hold.

## Change

Response shaping only. No read, query, graph or schema change.

- New request argument `evidence_detail`, `full` or `handles`, using the same two
  strings as the context routes (`querycontract.ContextEvidenceDetailFull` and
  `ContextEvidenceDetailHandles`). Invalid value: HTTP 400. HTTP default `full`;
  the MCP route (`go/internal/mcp/code/dead/routes.go`) sends `handles` when the
  argument is absent, including when `consumer_repo_ids` is named, and an explicit
  value wins.
- Under `handles` (`cross_repo_evidence_detail.go`, run after bucketing) each row's
  `consumer_evidence` becomes at most 5 group objects `{consumer_repo_id,
  relationship_type, evidence_family, confidence_label, item_count}` grouped by
  those three keys, `confidence_label` from the group's strongest item. Order:
  max confidence desc, `item_count` desc, then the three keys ascending, so the
  group that decided `live_by_consumer` is first and is never cut.
- `consumer_evidence_count` is still the items the row held;
  `consumer_evidence_group_count` is the groups before the cap;
  `consumer_evidence_handles_truncated` is `true` only when groups were cut.
- `data.boundary_consumer_evidence` becomes the same five-key objects, one per
  item, strongest first, capped at 25. `boundary_consumer_evidence_count` stays the
  total; `boundary_consumer_evidence_truncated` is `true` only when cut.
- `data.evidence_detail` is always set. When something was reduced,
  `data.evidence_detail_drilldown.full_rows` says how to get it back and
  `truth.omissions` carries `candidate_buckets.consumer_evidence` (sum of
  `consumer_evidence_count`) and `boundary_consumer_evidence` (the boundary total),
  each `detail: "handles"`.
- Classification is computed from the in-memory evidence before shaping. Rows are
  copied before they change, so no shared map is mutated.

Refused alternatives: lowering the default `limit`, raising the budget, cutting
rows or items without a count and marker, and tuples.

## No-Regression Evidence:

Metric: est2x, the serialized MCP tool result with both wire copies, against the
262,144-byte dispatch budget. For a reply the dispatcher already moved to
resource-only, est2x is re-rendered with both copies (`twoCopyBytes` in the test);
`estimateResponseBytes` alone would report one copy. Real `dispatchTool` and real
HTTP handler, default MCP arguments (limit 25).

### The bar: calibrated base

Fixture: `newDeadCodeBudgetStore` (the #7168 base: 400 candidates, 60-byte
docstrings, suppressed bucket full) with the pathological evidence: 24 rows of 40
items each in 40 distinct consumer repositories (production-shaped ids, about 616
bytes per item in full), one fallback row, and a 60-item boundary list. Test:
`TestFindCrossRepoDeadCodeHandlesCalibratedBaseBars`
(`go/internal/mcp/dispatch_cross_repo_handles_calibrated_test.go`).

Bar (a): evidence adds at most 45,875 bytes (17.5% of the budget) over the same
base with no evidence. Bar (b): est2x <= 262,144 with `structuredContent`
delivered. The caps are 5 groups per row and 25 boundary items.

| Run | est2x | Class | (a) contribution over 164,116 | Result |
|---|---|---|---|---|
| Base, no evidence | 164,116 | delivered | | |
| Pathological, caps 10 / 50 (first attempt) | 255,184 (97.3%) | delivered | 91,068 | (a) fails |
| Pathological, caps 5 / 25, `handles` (MCP default) | **209,824** (80.0%) | delivered | **45,708** | (a) passes, 167 bytes under the ceiling; (b) passes |
| Pathological, `full` | 1,482,262 | over budget | | |
| Group cap off (seeded violation) | 466,432 | resource-only | 302,316 | (a) and (b) fail |
| Boundary cap off (seeded violation) | 221,494 | delivered | 57,378 | (a) fails |
| Both caps off (seeded violation) | 478,102 | resource-only | 313,986 | (a) and (b) fail |
| Projection off (seeded violation) | over budget | over budget | | (b) fails |

The 167-byte margin on (a) is thin. About 5.5 KB of the 45,708-byte contribution
comes from the rows moving from `dead` to `unknown`, not from evidence, so an
unrelated per-row field of a few bytes can fail the bar as well; any base or
fixture change that adds bytes to the evidence path will fail the bar and should be read as the cap needing to drop
further (3 groups), not as a bar to move. A review probe with the caps at 3 and 1
reported lower numbers than K=5 at the 60-byte base (203,440 and 188,656).

### Docstring sensitivity (item e)

The evidence term is independent of the docstring, but the row base grows with it
because every docstring is echoed about six times per row. With the caps at 5 / 25
and the pathological evidence:

| Docstring bytes | est2x | Class |
|---|---|---|
| 60 | 209,824 | delivered |
| 100 | 231,824 | delivered |
| 150 | 259,324 | delivered |
| 155 | 262,074 | delivered |
| **156** | 262,624 | **resource-only** (first length past the budget) |
| 200 | 286,824 | resource-only |
| 512 | 458,424 | resource-only |

So the calibrated base with this evidence stays delivered up to a 155-byte
docstring on every row and goes resource-only from 156 bytes. A review probe at
the first caps (10 / 50) reported the same transition at 80 bytes.

The promise this change makes: evidence under `handles` is bounded to at most
17.5% of the budget. It does not claim every repository fits with
`structuredContent`: the row base is clipped at 512 bytes per docstring but echoed
about six times, so above the threshold above the reply is delivered resource-only
(the full payload, `structuredContent` omitted) until the echo dedupe lands.

### Heavy rows (item c)

Fixture: `heavyRowStore` (long paths and names, decorators, 2,000-byte docstrings
that the read-time clip cuts to 512 bytes, python suppressed rows of the same
weight), with and without the pathological evidence. HEAD plus part A returns the
full payload, so `full` is the comparison.
`TestFindCrossRepoDeadCodeHandlesNeverWorsensHeavyRowOutcome` requires the
`handles` class to be no worse.

| Heavy fixture | `full` (HEAD + A) | `handles` (B) |
|---|---|---|
| No evidence | resource-only, 477,164 est2x | resource-only, 478,920 est2x |
| Pathological evidence | over budget, 1,795,916 bytes | over budget, 523,478 bytes |

The no-evidence heavy base is already resource-only, so no evidence cap can
deliver it; the 1,756-byte difference is mostly `consumer_evidence_group_count: 0`
on every row (about 70 bytes per row across both copies), plus the new
`evidence_detail` and boundary keys. With the pathological evidence both are over budget, but `handles` is about
1.27 MB smaller. No class gets worse, so the row-base echo dedupe is not pulled
into this change.

### Evidence isolation (thin base)

The thin-base fixtures (`dispatch_cross_repo_handles_budget_test.go`: no
suppressed rows, no padded docstrings) isolate the evidence term and are not the
bar. At caps 5 / 25 the pathological thin fixture is 116,274 est2x (delivered),
and the same fixture with 40 items across 3 repositories per row is 99,474 (3
groups per row, so no row carries `consumer_evidence_handles_truncated`). That
99,474 is a thin-base number, not a statement about a real repository.

For the first caps (10 / 50) the thin pathological fixture measured 161,634. With
the group cap seeded off (boundary cap still 50), 201,237 was the one-copy
resource-only size the dispatcher emitted, not the est2x. With both caps off the
thin fixture measures 384,552 est2x (202,961 one copy; a review probe reported
381,282). The earlier note that cap-off left the thin bar only 4.6 KB over was
wrong: it compared a one-copy size to a two-copy bar.

The pathological numbers are synthetic fixtures, not a live reply; the live
QA number is NOT_CHECKED (no QA access in this worktree).

Classification invariance:
`TestCrossRepoDeadCodeHandlesDoesNotChangeClassification` runs the same request
under `full` and `handles` and requires equal `bucket_counts`, `analysis`, and
every row beyond the consumer evidence fields (classification, reasons, hidden
counts, labels), across a live row, a hidden-consumer row and a boundary row.

Golden corpus: the MCP query shape in `testdata/golden/e2e-20repo-snapshot.json`
asserted `live_by_consumer[].consumer_evidence[].citation`, which a handles reply
never carries. It now requires `...consumer_evidence[].consumer_repo_id` and
`...confidence_label` and pins `data.evidence_detail: "handles"`; the HTTP shape
keeps `citation`. The mirror fixtures moved together
(`go/cmd/golden-corpus-gate/mcp_test.go`, `snapshot_test.go`,
`go/internal/goldengate/evaluate_test.go`). The snapshot test fails against the old
snapshot and passes against the new one. The live B-7 gate is NOT_RUN (it needs
Docker and fixed ports).

## No-Observability-Change:

Response shaping on the handler's output rows only. No new span, metric or log.
The existing `SpanQueryDeadCodeInvestigation` span and the MCP dispatch budget
metrics (`response_bytes`, over-budget counter) already report the effect: an
operator sees the `mcp_response_over_budget` rate for `find_cross_repo_dead_code`
fall, and `truth.omissions` plus `data.evidence_detail` on each reply say what was
reduced.
