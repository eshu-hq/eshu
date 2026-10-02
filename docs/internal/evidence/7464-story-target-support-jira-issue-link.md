# Story target_support: Jira record and transition link (#7464)

Before this change a `work_item.record` or `work_item.transition` never appeared
in a repository or service story's `target_support`, and always counted as
source-only: neither fact kind carries a repository. The one durable key in the
Jira writers is the issue id, `provider_work_item_id`, which a record, its
transitions and its remote links all take from the same `Issue` in one loop with
one `EnvelopeContext` (`collector/jira/source.go`, `client.go`, `envelope.go`).
A repository link, `work_item.external_link` with `linked_repository_id`, therefore
names the repository for every record and transition of its issue.

## Join

A record or transition attaches to repository R when its issue has a
`work_item.external_link` that is non-tombstoned, names R in
`linked_repository_id`, and sits in the same `scope_id` and the same active
generation, with a non-blank `provider_work_item_id` on both sides.

- The key is the issue id only. `work_item_key` changes when an issue moves.
- The join never crosses scopes (the Jira issue id is unique per site) or
  generations.
- The id guard is mandatory: the transition and link envelopes do not validate
  `provider_work_item_id`, so a `'' = ''` join would link every id-less row of a
  scope. A transition and a link with a blank id and a link naming a fourth
  repository are in the matrix.
- An issue with N links to R yields its record once and each transition once; the
  SQL carries the lowest qualifying link id as `linked_via_fact_id`, and the Go
  re-check (`support.JiraFactLinked`) accepts a derived row only with that
  witness, a non-blank issue id, a record or transition kind, and the stamped
  `linked_via_repository` equal to the story's repository (the same check
  `RoutingFactCorrelatedTo` makes for a PagerDuty row), so a row read for another
  repository never counts.
- An issue linked to R and R2 is evidence in both stories. Metadata kinds and
  `metadata_warning` stay source-only.
- A service target applies the #7480 DEFINES gate to the derived rows unchanged:
  `repository_sole_workload` evidence, `repository_multiple_workloads` ambiguous,
  anything else closed.
- `link_basis` for a repository target is `issue_linked_repository` (links keep
  `linked_repository`, PagerDuty rows `incident_repository_correlation`).
- Order: links and PagerDuty rows first, then records, then transitions, each
  newest first. All facts of a generation share one `observed_at`, so the old
  order degenerated to fact id, and an issue can carry a hundred transitions
  (`collector/jira/client.go`); without the rank they would crowd out the links
  that justify them in a ten-row section.

The source-only count changes with it: a record or transition whose issue has a
live repository link is linked to some repository, so it is neither this target's
evidence nor source-only (`support.LinkedIssuePredicate`, two-valued: the issue id
is wrapped in `COALESCE` and the set holds no blank id).

The scoped `/api/v0/work-items/evidence` predicate is unchanged and still fails
closed; `go/internal/query/work-item-evidence-scope-evidence.md` records why the
story needs no new grant logic (arbiter ruling, #7464).

## Shapes measured

Performance Evidence: PostgreSQL 18.6, one disposable database, the #7138 corpus
(200 repository scopes of three generations of 1,000 facts, support kinds about
1%) plus one Jira scope of three generations (active generation full size,
superseded ones a tenth). Medians of 7 runs after a discarded warm-up, `PREPARE`
with `plan_cache_mode` forced to custom and generic (the two agree within noise
below; one column is shown). The host ran other work at load average 8 to 22, so
the milliseconds are indicative and the buffer counts are the stable figures. The
unindexed 952 ms and 1,038 ms rows are single `EXPLAIN (ANALYZE, BUFFERS)` runs.

| Scale (one scope, active generation) | Statement | Shape | Time | Buffers |
|---|---|---|---:|---:|
| 20k issues, 50k links (500 to R), 30k transitions | row read | no issue index (second hop heap-filters the issue id) | 952 ms | 344,516 |
| same | row read | index on `(scope, generation, issue id)` without the kind | 13.9 ms | 8,316 |
| same | row read | final: kind as a key column, records then gated transitions | 9.7 ms | 6,959 |
| 200k issues, 500k links, 300k transitions | row read | final | 37.6 ms | 22,366 |
| 5,000 issues, every link to R, 100 transitions each | row read | no kind in the index, one statement | 1,038 ms | 525,609 |
| same | row read | no kind in the index, records then gated transitions | 217 ms | 525,618 |
| same | row read | final (kind as a key column, records then gated transitions) | 42 to 51 ms | 25,609 |
| 20k issues, 50k links, first run | source-only | before (no Jira issue term) | 45 ms | 12,443 |
| same | source-only | `EXISTS` per record/transition | 93 ms | 28,367 |
| same | source-only | `MATERIALIZED` linked-issue set, hashed `IN` (shipped) | 72 ms | 19,863 |
| same, rerun after reseeding (more dead tuples) | source-only | before / shipped | 72 ms / 108 ms | 33,151 / 40,964 |
| 200k issues, 500k links | source-only | before / shipped | 256 ms / 529 ms | 69,006 / 126,899 |
| plan-proof corpus (`TestServiceStoryJiraIssueLinkUsesIssueIndexLive`) | row read | final | 4.4 to 4.7 ms | about 4,070 |
| same | source-only | before / final | 26 ms / 48 to 51 ms | 18,006 / 31,000 to 32,500 |

What decided the shapes:

- The second hop has no existing index: no migration keys `provider_work_item_id`.
  Probed through `fact_records_story_support_kinds_idx`, each linked issue reads
  every record and transition of the generation and discards the others, so the
  cost is linked issues times facts per scope. Migration 155 fixes it.
- Without the kind in the index key, one probe returns an issue's record and its
  transitions together and discards all but the record, which is the worst-case
  525k buffers, with or without the transition gate. With the kind as a key column
  and the transition probe gated on a one-time filter, the worst case reads only
  the records of the linked issues (25,609). A single statement over both kinds
  with the kind-keyed index was not measured.
- The source-only subtraction must exist (an overstated count is a wrong answer),
  so its cost is the price of correctness. A hashed `IN (SELECT ...)` over a
  `MATERIALIZED` set beat a correlated `EXISTS` on buffers (19,863 against 28,367
  on the first corpus). An index-only linked-issue set (a second partial index
  carrying the repository condition) cut the 500k-link time by about a quarter and
  was not shipped: it would add a second migration and index for a count that runs
  only when a story found no evidence.
- The cost grows with active links, like the count it extends: about twice the
  baseline at 500k links in one scope.

Ingest cost: the index holds one entry per non-tombstoned record or transition and
none for other kinds. Its size and write overhead at production volume were not
measured.

## Correlation truth matrix

`TestServiceStoryTargetSupportJiraIssueLinkMatrixLive` builds every fact with the
production Jira envelope writers (`NewWorkItemRecordEnvelope`,
`NewWorkItemTransitionEnvelope`, `NewWorkItemExternalLinkEnvelope`,
`NewWorkItemProjectMetadataEnvelope`) and reads through the shipped
`ContentReader`. On the branch before the change it failed: the repository story
returned only the four links, R2 only its two, and the source-only count was 14
where 7 is correct.

Positive: record, two transitions and two links to R (record once, witness is the
lowest link id); an issue linked to R and R2 (a record and transition in each
story); a service target whose repository defines exactly it.

Negative: an issue linked only to R2 (not R3's evidence, not source-only); an
issue with no link; an issue whose only link is tombstoned or sits on a superseded
generation; a blank issue id on a transition and a link (no join); the same issue
id linked only in another scope (the record stays source-only, and the other
scope's own link is still R's evidence); the plain link and the project metadata
row (source-only).

Ambiguous: a service whose repository defines several workloads, so every derived
row and link is ambiguous; a service the graph did not confirm, or one with no
defined workload, is closed. The row bound keeps links ahead of records and
transitions and reports `truncated`.

## Proof commands

```bash
cd go && go test ./internal/query/... -count=1
ESHU_POSTGRES_DSN=postgres://<user>@<host>/<disposable> \
  go test ./internal/query -run 'JiraIssueLinkMatrixLive|PagerDutyRoutingMatrix|WriterShapedMatrix' -count=1
ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=postgres://<user>@<host>/postgres \
ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/query -run 'TestServiceStoryJiraIssueLinkUsesIssueIndexLive' -count=1 -v
cd go && go test ./internal/storage/postgres/migrations ./internal/storage/postgres -run 'Migration|Bootstrap|SchemaOrder' -count=1
```

Observability Evidence: the read keeps the `postgres.query` span (operation
`list_service_story_target_support`), now covering up to four bounded statements
(the link read, the PagerDuty routing read, this record and transition read, and
the source-only summary) on one read-only snapshot, and the evidence rows carry
`linked_via_fact_id`. The stage events already log
`target_support_evidence_count` and `target_support_ambiguous_count`; derived rows
count in them. No collector, reducer queue, graph write, metric instrument or
runtime flag changes.
