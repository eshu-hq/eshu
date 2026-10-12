# Lease-expiry proof evidence (#7939)

Performance Evidence: The local baseline and corrected expiry cell ran on
2026-10-11 at tested commit `17c48cbe8edbd355a0cccf1ea104379ae613ae87`.
Both used fresh PostgreSQL 18 Alpine and the Compose-pinned NornicDB
`fix-500-e022384c` backend. Inputs were the shared GCP, synthetic multiscope,
SQL, code-call, documentation, and rationale cassettes, with four workers.
The baseline took 17 seconds; the held-handler expiry cell took 14 seconds.
These are single functional runs, not a latency benchmark or speedup claim.
Both canonical graph digests matched; terminal residuals and dead letters
were zero. The corrected cell increased reclaim from zero to one and proved
replacement attempt two before releasing the original handler.

Observability Evidence: The controlled original cell observed a claim, then
committed its ACK before the expiry statement. The statement affected zero
rows and the unchanged reclaim assertion failed. This proves an ineffective
injection schedule is possible; historical hosted ACK timing remains unproven.
The corrected cell records held, expired, replacement, release, and finished
identities plus the owner marker. Failure uploads retain these bounded records.
The untagged constructor preserves executor identity and ignores the barrier
environment. The enabled hook holds one handler outside graph permits and
transactions; race tests prove unrelated and replacement work can progress.
Production worker count, lease, heartbeat, SQL fencing, and blocking checks
remain unchanged. The evidence supports the fault seam and convergence, not
production throughput or a full local run of all 49 cells.

Stale ACK observation (#7928): after the drain, cell 3 calls
`ifa_fault_assert_stale_ack_fenced` and `ifa_fault_assert_held_item_terminal`
in `scripts/lib/ifa_fault_lease_expiry_barrier.sh`. After the drain the cell
reads the real reducer's log for the held work item and requires at least one
fenced-outcome record (`ack_claim_rejected`, `ack_outcome_unknown`, or
`lease_lost_during_execution`), at most one `succeeded` record, and at least
two ACK outcome records in total; it then reads the queue row and requires
status `succeeded` at the replacement claim (attempt_count = held + 1, a new
last_attempt_at, lease cleared). A stale ACK accepted after the successor's
own ACK would add a second `succeeded` record and fail the cell. When the
stale and the successor ACK share one flush batch, the Service logs both as
`ack_outcome_unknown` and writes no `succeeded` record for the item
(#8007); the log carries no claim epoch, so the check does not
attribute a record to the stale claim or the successor. Epoch-level rejection
against a live successor is proven by `TestReducerQueueExpiryInterleaveLive`
(class scheduled). `TestReducerQueueExpiryInterleaveLive` also requires that
the stale ACK leaves the successor's full queue row and the
`cross_scope_completion_events` count unchanged.
