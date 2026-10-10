# #7407 Failure Text Bound At The Fail-Time Writer

`fact_work_items.failure_details` and `failure_message` were stored at any
length: `QueueFailureMetadata` and `DeadLetterTriageMetadata`
(`go/internal/storage/postgres/queue/failure_metadata.go`) wrote the sanitized
error text or `FailureDetails()` unbounded, through all four callers
(`ProjectorQueue.Fail`, `ReducerQueue.failIntent`, retry and dead-letter each).
#7320 keeps that text when a row is superseded, so a wide value is copied once
more per superseded row.

## Change

- `failure_details` is capped at 4,096 bytes (`MaxFailureDetailsBytes`) and
  `failure_message` at 1,024 bytes (`MaxFailureMessageBytes`). `failure_class`
  is not bounded; it only holds constants.
- The cap is applied in Go, after `sanitizeFailureText`, so the counts describe
  the sanitized text that would have been stored. It is not in SQL and not in
  the #7320 fold, so the stored evidence and its `prior_failure` copy stay equal.
- Text over the limit keeps a prefix cut on a rune boundary and ends in
  `...[truncated: <original> bytes, kept <n>]`. The marker counts toward the
  limit: the stored value never exceeds it.
- The dead-letter path's "did the cause supply its own details" test now compares
  the unbounded sanitized text. Comparing the bounded values would have made a
  plain error with a message over 1,024 bytes look self-detailed, because the two
  limits differ, and would have dropped the structured triage details.

The limits come from the arbiter ruling for the lane. Where the numbers come
from: the only observed dead letter is 793 B of details and 645 B of message,
and the repository's existing dead-letter message bound is 4,096
(`storage/postgres/collector_generation_dead_letter.go`).

NOT_CHECKED: the read-only QA `octet_length` percentiles the issue asks for.
QA access is held by another lane and was not probed, so the ruled defaults
ship unchanged. Raise the details limit only if a legitimate p99 is over 4,096.

## Proof

Hermetic (`queue/failure_metadata_test.go`), each of the six bounding tests
failed against the old writer with the constants defined:

| Test | Asserts |
| --- | --- |
| `TestQueueFailureMetadataBoundsDetails` | 10,000 B of details stored at most 4,096 B, prefix unchanged, marker names 10,000 and the kept length, class and in-limit message unchanged |
| `TestQueueFailureMetadataCutsOnRuneBoundary` | a cut inside a run of two-byte `é` stays valid UTF-8 and keeps an even byte count |
| `TestQueueFailureMetadataBoundsMessageAndKeepsClass` | a 5,000 B error stored at most 1,024 B with the marker, fallback class unchanged, details keep more than the message |
| `TestQueueFailureMetadataLeavesValuesAtTheLimitUntouched` | exactly 4,096 / 1,024 B is unchanged with no marker; one byte over is bounded |
| `TestQueueFailureMetadataBoundsAfterSanitizing` | NUL and invalid UTF-8 are stripped before the count |
| `TestDeadLetterTriageMetadataBoundsTriageDetails` | the structured triage string is bounded the same way |
| `TestDeadLetterTriageMetadataKeepsSelfDetailsWhenTheyDifferFromTheMessage` | guard for the comparison above; passes on main and after |

Live, Postgres 18 (`postgres:18-alpine`), `TestFailBoundsWideDetailsAndSupersedeFoldCopiesTheStoredString`
(`ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN`): a `ProjectorQueue.Fail` with 10,000 B
of details dead-letters a row; the stored `failure_details` is at most 4,096 B and
carries the `10000 bytes` marker; a claim then supersedes the row and
`prior_failure.failure_details` equals the stored string byte for byte, with
`prior_failure.failure_class` unchanged. Against the old writer it fails at the
first assertion (`stored failure_details = 10000 bytes, limit 4096`).

## Performance

No-Regression Evidence (#7407): the touched path is the Fail-time writer, which
runs once per failed or retried work item, off the claim path, with no SQL change.
Go benchmark `BenchmarkQueueFailureMetadata`, 200,000 iterations, 3 runs, Apple
M-series, old writer against new:

| Cause | Old | New |
| --- | --- | --- |
| in limit (800 B details, no truncation) | 865 to 945 ns, 32 B, 2 allocs | 815 to 827 ns, 32 B, 2 allocs |
| 64 KB details, bounded to 4,096 B | 43.7 to 44.0 us, 32 B, 2 allocs | 44.3 to 44.8 us, 4,256 B, 9 allocs |

The in-limit path, the common one, is unchanged. The 64 KB case is dominated by
the existing sanitize scan; the bound adds about 1% and one 4 KB copy.

The nanosecond figures were taken while the host load average was 26 to 44 and
are noisy: a reviewer's rerun at load 39.7 gave 1,124 ns and 84,899 ns. The
allocation columns do not depend on load and match on every run, so the
no-regression reading rests on them, on the unchanged in-limit path (no
truncation branch runs), and on there being no SQL change.

The reason for the bound is the write cost the #7320 fold pays per superseded
row. `TestSupersedePriorFailureWriteCost` (`ESHU_7320_COST_PROOF=1`, a dedicated
Postgres 18 with `pg_stat_statements` preloaded and autovacuum off, 500 rows per
scenario, mean of 3, steady regime) measures bytes written, which do not depend on
host load. Delta the fold adds per superseded row, two of the five supersede
writers:

| Writer | Details width | WAL bytes | Pages dirtied |
| --- | --- | --- | --- |
| Heartbeat supersede | 4,096 B (the new bound) | +4,692 | +0.66 |
| Heartbeat supersede | 65,536 B (unbounded) | +70,143 | +8.33 |
| Ack refusal | 4,096 B | +4,692 | +0.66 |
| Ack refusal | 65,536 B | +70,143 | +8.33 |

At the bound the fold's worst-case WAL per superseded row is about 15 times
lower than at 64 KB, and its dirtied pages about 12 times lower. The 800 B
QA-scale rows are below the bound and unchanged (+986 WAL bytes per row for
Ack refusal). Wall-clock cost was deliberately not used: the host load average
was 26 to 44 during the session, so the harness's timing run was abandoned and
only byte counts are reported. The other three writers were not re-measured.

Observability Evidence: the truncation is visible in the stored row itself. An
operator reading `failure_details` or `failure_message` in the admin list, the
status view or `prior_failure` sees `...[truncated: <original> bytes, kept <n>]`
and the original size, so a truncated row cannot be mistaken for a complete
one. This change adds no metric, span or log key; the dead-letter counter is
#7386 and the `prior_failure` wire exposure is #7385.
