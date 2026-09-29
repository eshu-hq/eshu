# #7388 Reclaim And Operator-Note Writers Keep The Failure They Replace

#7320 keeps a work item's failure evidence when it is superseded. Two other
writers rewrote the failure fields of a row without keeping it.

## What each writer did

`projector_stale_scope_reclaim` (`projector_queue_claim_sql.go`, the
`reclaimed_stale_projector_duplicates` and `reclaimed_claim_siblings` CTEs, alias
`stale`) moves an expired claimed or running row back to `retrying` under a fixed
class and message and replaced `failure_details` with its own bookkeeping ids. The
cause the last attempt failed with was gone. A reclaimed row is later retried,
failed, or superseded, and nothing kept what the reclaim erased. The status
`latest_failure` and the lifecycle drilldown read the reclaim marker instead of the
real class for the same reason.

The operator note (`query/admin/store/postgres.go`, `DeadLetterWorkItems` and
`SkipRepositoryWorkItems`, alias `work`) replaced `failure_details` with the note.
The issue said this overwrites a live row. That is true only of Skip, and only for
rows that are not leased: dead-letter acts on `failed` and `dead_letter` rows, and
skip acts on unleased `pending`, `retrying` and `failed` rows and dead-letters them
in the same statement. Neither rewrites a row that stays claimable, and both keep
the class and message. What the note lost was the details the row failed with: the
triage details of a terminal row, or the retry-time details of a `pending` or
`retrying` row that skip dead-letters. Skip also had
`COALESCE(work.failure_class, 'operator_skipped')` where the dead-letter path has
`COALESCE(NULLIF(work.failure_class, ''), ...)`, so an empty class survived a skip.

## Change

- Both reclaim UPDATEs keep the marker class and message and assign
  `failure_details = CASE WHEN stale.failure_class = 'projector_stale_scope_reclaim'
  THEN stale.failure_details ELSE (jsonb_build_object(<today's keys>) ||
  priorFailureStaleSQL)::text END`. A row that already carries the marker keeps its
  details as they are, so a second reclaim does not nest the first one's bookkeeping
  one level deeper. No migration, no new column, no reducer change.
- Both operator-note statements assign `CASE WHEN NULLIF($n, '') IS NULL THEN
  work.failure_details ELSE (jsonb_build_object('operator_note', $n::text) ||
  PriorFailureWorkSQL)::text END`. `PriorFailureWorkSQL` is the #7320 work-alias
  fold, exported from `storage/postgres` for the admin store. An empty note leaves
  the details untouched.
- Skip's class assignment takes the same `NULLIF` the dead-letter path has.
- Retry and Fail still overwrite the failure fields once per attempt, by design. No
  append-only history table.
- A row reclaimed twice keeps the first reclaim's details as they are, so
  `claimed_work_item_id` (sibling reclaim) is the first reclaim's, and is absent when
  the first reclaim came through the duplicate path. Nothing in Go reads those keys.

## Proof

Each test failed against the old SQL first.

| Test | Asserts |
| --- | --- |
| `TestProjectorClaimReclaimKeepsPriorFailureDuplicate` | the first reclaim UPDATE: `retrying`, marker class and message, `failure_details.prior_failure` = the seeded status, class, message, details and `updated_at`, own keys unchanged |
| `TestProjectorClaimReclaimKeepsPriorFailureSibling` | the second UPDATE, same, with `claimed_work_item_id` still present |
| `TestProjectorClaimReclaimDoesNotNestAnAlreadyReclaimedRow` | a row already carrying the marker keeps its details byte for byte |
| `TestProjectorClaimReclaimThenSupersedeKeepsTheRealCause` | reclaim, then a claim sweep supersedes the row: `prior_failure.failure_class` is the marker and its details parse to JSON whose `prior_failure.failure_class` is the real class |
| `TestReclaimStatementsFoldPriorFailure`, `TestReclaimWriterGuardSeededViolation` | a source-enumerated guard (like #7320's): every `fact_work_items` UPDATE that assigns the marker embeds `priorFailureStaleSQL` in its `failure_details` assignment. The seeded case reports a writer without the fold, the wrong alias's constant, a comment naming it, and the constant in another assignment |
| `TestDeadLetterWorkItemsFoldsPriorFailureUnderTheOperatorNote`, `TestSkipRepositoryWorkItemsFoldsPriorFailureUnderTheOperatorNote` | the statement text wraps the note around the fold, an empty note keeps the details, Skip has the `NULLIF` |
| `TestOperatorNoteKeepsTheFailureItReplaces` (live, real schema) | dead-letter of a failed row with details D and note N stores `operator_note` = N and `prior_failure.failure_details` = D with class and message unchanged; an empty note leaves D; skip does the same for a retrying row; an empty class becomes `operator_skipped`; a never-failed row gets no `prior_failure` |

The five Postgres proofs and the existing #7320 suites (46 live tests, including
`TestSupersedeNoCandidateClaimRunsSamePlanAndWritesNothing`) pass on Postgres 18.

The #7320 cost and WAL harness derived its "before" text by cutting the first use
of the fold. The reclaim UPDATEs now put two more uses ahead of the supersede CTE,
so it cuts every use instead; the reclaim uses are inert in the #7320 seeds, which
hold no expired duplicate lease.

## Performance

No-Regression Evidence (#7388): the reclaim UPDATEs are inside the projector claim
statement, a hot path, but they write only rows whose lease expired beside a live
one. The statement's plan with no reclaim candidate is unchanged
(`TestSupersedeNoCandidateClaimRunsSamePlanAndWritesNothing`, run against the
statement with every fold cut out). When rows are reclaimed the fold adds bytes
written per reclaimed row. `TestReclaimPriorFailureWriteCost`
(`ESHU_7320_COST_PROOF=1`, a dedicated Postgres 18 with `pg_stat_statements`
preloaded and autovacuum off, 500 rows, mean of 3, before derived from the shipped
constant by reversing the change, so it cannot drift):

| Details width | Regime | WAL bytes per row | WAL records | Pages dirtied per row |
| --- | --- | --- | --- | --- |
| 800 B | steady | 932.7 to 1,916.9 (+984.1) | 7.17 to 7.17 | 0.064 to 0.186 |
| 800 B | post-checkpoint | 2,372.3 to 3,356.4 (+984.1) | 7.17 to 7.17 | 0.262 to 0.384 |
| 4,096 B (the #7407 bound) | steady | 1,099.2 to 5,795.5 (+4,696.3) | 10.17 to 16.18 | 0.064 to 0.734 |
| 4,096 B | post-checkpoint | 5,855.3 to 10,618.5 (+4,763.2) | 10.17 to 16.18 | 0.816 to 1.496 |

That is the same per-byte cost the #7320 supersede fold pays. #7407 (PR #7452, open
when this was written and ordered ahead of this change) caps the text the folds
copy at 4,096 bytes; until it lands they copy details of any size. The 4,096 B rows
above are the cost at that cap. Wall time is not reported: the host load average
was 26 to 44. NOT_CHECKED: the operator-note statements' write cost. They run once
per operator call over the filter's limit of rows (100 by default) and are not on
any claim path.

Observability Evidence: no new signal. The reclaim marker class and message are
unchanged, so every reader that pins them still works. The kept evidence is in the
row's `failure_details`, and its wire exposure (`prior_failure` on the freshness
lifecycle and the admin listing, with `operator_note`) is #7385.
