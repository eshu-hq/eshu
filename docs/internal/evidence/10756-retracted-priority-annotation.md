# Retracted findings annotate their priority output (#10756)

A finding whose advisory a source rejected or withdrew used to read as an
ordinary scored finding in its priority fields. It now carries a zero-weight
`advisory_retracted` contribution and a `priority_reason` that leads with the
retraction. This note records the decision, the cost, and how an operator sees
it.

## Decision

Annotate, never move. `priority_score` and `priority_bucket` stay as computed.
Zeroing or capping the score would change the first ORDER BY key of canonical
winner selection and of the paged reads, so a re-derived retracted copy would
lose to a stale active copy in another holder scope until fan-out reaches it.
A new bucket member would extend a closed enum on the OpenAPI response, two
filter enums, `ValidPriorityBucket`, and the MCP enums. The annotation touches
none of those.

## What changed on the hot path

- `withSupplyChainImpactPriority` appends one contribution row after the score
  is clamped, when `finding.AdvisoryRetraction` is set. The row has zero weight,
  so the sum of contributions still equals the clamped score.
- `supplyChainPriorityReasonCodes` adds `advisory_retracted` from that row. It
  needed no change.
- `supplyChainPriorityReason` opens with the retraction for a retracted finding.
- Retraction is attached before every call that computes priority
  (`product.go`, `impact_classify.go`, and the re-runs in `derived_conflict.go`),
  so every path sees it. The rebuild starts from the contributions each time, so
  a re-run does not duplicate the row.

## What this does not claim

A retracted record lends no CVSS severity on the package path and the product
(CPE) path alike, so a retracted finding scores from its other evidence. The
product path used to keep the rejected record's CVSS when every record in the
group was retracted; eshu-hq/eshu#10764 closed that (see the follow-up below).
The annotation makes the retraction legible; it does not zero or cap a score.

## Measurement

No-Regression Evidence: `BenchmarkBuildFindingsMultiSourceGroups` in
`go/internal/reducer/supplychain/core/retraction_bench_test.go` builds findings
for 500 three-source CVE groups, live and all rejected. Base is `origin/main`
(`a4a8e01976`) in a throwaway worktree; head is this branch. Five rounds,
base then head each round, `-benchtime 50x`, `go1.26.11`, darwin arm64, a
shared machine with other sessions running, so timings are noisy.

| Case | Tree | Median ns/op | Min ns/op | B/op | Allocs/op |
| --- | --- | --- | --- | --- | --- |
| live | base | 11,241,691 | 9,450,836 | 16,012,036 | 129,691 |
| live | head | 9,811,663 | 8,612,433 | 16,012,042 | 129,692 |
| all_rejected | base | 10,705,622 | 9,599,160 | 16,858,309 | 141,699 |
| all_rejected | head | 9,604,930 | 9,316,598 | 16,940,783 | 142,205 |

The live case is unchanged in bytes and allocations. The all_rejected case adds
about 82 KB (0.5%) and 506 allocations for 500 findings, close to one extra
allocation per retracted finding. Head timing
is not slower than base in either case; the spread between rounds is larger than
any difference, so no speedup is claimed.

Terminal state is unchanged: the same findings are built, the same fact count is
written, and the content digest of a retracted row changes once because the
payload carries the new contribution, so each retracted row rewrites on the next
reducer pass that derives it.

No-Observability-Change: no new metric, span, or log key. The row rides the
existing `priority_contributions` and `priority_reason_codes` payload fields on
the `reducer_supply_chain_impact_finding` fact. An operator finds retracted
findings with `suppression_state=advisory_retracted` and
`include_suppressed=true`, or by `advisory_retracted` in `priority_reason_codes`.

## Proof

Run in `go/`:

`go test -run 'TestRejectedAdvisoryWithReplacement|TestUnrejectedAdvisoryKeeps|TestRetractedProductFinding|TestSupplyChainImpactPriorityModel' ./internal/reducer/supplychain/core`

## Follow-up issues

The review of this change found two behaviors outside its scope. Both are filed
and worked in the same goal.

- eshu-hq/eshu#10764 (fixed on its own branch): a product-keyed (CPE) finding
  whose only record was rejected scored CVSS from the rejected record, while a
  package-keyed one did not. The product path now blanks the score, vector, and
  label of an all-retracted group, keeping the representative's `FactID` and
  identity. For a rejected-only product finding at CVSS 9.8 the priority score
  went from 100 (critical) to 60 (high) in the test fixture, because the +60
  `cvss` contribution is gone. Review found the same leak through a stale copy:
  two copies of one record, the older Analyzed at 9.8 and the newer Rejected,
  still scored 9.8 because `severityObservation` read the raw observations while
  the retraction read the newest copy. It now reduces to the newest copy first
  (`newestCopyAdvisoryInputs`), so the rule reads "a group whose every record,
  after newest-copy reduction, is retracted". Behavior note: live-vs-live rows
  sharing one (source, advisory id) are copies of one record, so a recency tie
  now resolves to the greater fact id, as the package path already does.
- eshu-hq/eshu#10773: a rejected record beside a quarantined (unreadable)
  sibling leaves the finding active and unannotated, because `retractionFor`
  cannot tell what the unreadable record asserts, while the rejected record
  still lends no severity. The ruling keeps that on both paths: the finding is
  visible and scores 60 in the fixture, the same as its retracted twin, where
  main's product path scored 100 from the rejected 9.8. The
  readable-sibling and all-retracted shapes are pinned beside it in
  `TestRejectedRecordBesideASiblingLendsNoSeverityInEveryState`, and the
  package path in `TestQuarantinedLiveCVEFactDoesNotLetARejectionRetract`.
- eshu-hq/eshu#10765: a stale active copy in another holder scope can outrank a
  re-derived retracted copy in the default view, because winner selection orders
  by `priority_score` first. This change deliberately leaves score and bucket
  alone, so it neither causes nor closes that race.
  The race is bounded by the holder fan-out, one holder pass, and one winners
  resweep tick (see [9695b-holder-fanout.md](9695b-holder-fanout.md)); the
  winners audit does not shorten it. A holder the fan-out does not reach keeps
  its stale copy until its own next pass.
  `eshu_dp_supply_chain_impact_stale_copy_repairs_total` counts the
  retraction-state flips passes write, not the copies still stale (#10765).
