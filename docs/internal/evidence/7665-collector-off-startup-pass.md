# 7665: collector-off single-shard startup pass — performance note

The #7665 fix changes one wiring predicate
(`AfterEmptyBatchDrained: config.RepoShardCount > 1 || !scheduledSyncConfig.Enabled`
in `go/cmd/ingester/wiring.go`): a single-shard ingester with scheduled sync
off now enables the collector's empty-batch escape, so the deferred
relationship maintenance pass runs exactly once at startup instead of never.
The pass body, the barrier, and the collector poll loop are untouched; only
the trigger condition for that one configuration changes.

## No-Regression Evidence:

Baseline: origin/main before this change — single-shard collector-off
ingester never runs the pass (the #7665 stall); every other configuration
runs it exactly as before.
After: that configuration gains exactly one startup pass; all other
configurations are byte-identical in behavior (pinned by
`TestBuildIngesterCollectorServiceKeepsEmptyBatchEscapeOffForSingleShardScheduledSync`
and the unchanged multi-shard wiring test).
Measurement 1 (deterministic composition,
`TestIngestionStoreShardDrainBarrierSingleShardQuietRestartRunsExactlyOnePass`):
real `collector.Service.Run` + real single-shard barrier over 25 idle polls
drives 25 hook calls, 1 opener call, and exactly 1
`deferred_backfill_completed` line — the 24 later polls return nil from the
barrier short-circuit with no database call, so the steady-state per-poll
cost of the newly enabled escape is one nil-returning function call.
Measurement 2 (live, throwaway probe since removed): the full pass against
local PostgreSQL 18.6 (Debian 18.6-1.pgdg13+2) in Docker, isolated bootstrap
schema, one seeded scope with an active generation and one repository fact,
completes in 0.03 s with `evidence_facts=0 readiness_rows=1`. Terminal
counts: 1 pass, 1 readiness row published, 0 errors; probe schema dropped
afterwards.
Input shape: single active generation, no commits — the collector-off shape,
scaled down (production counts are symptoms, not reproduced).
Why safe: the added work is one idempotent, corpus-wide pass per process
lifetime in a configuration that previously did zero passes and stalled; the
once-latch plus the single-shard join-only short-circuit bound it to exactly
one (a per-poll storm would need the opener call to repeat, which the latch
forbids); multi-shard and sync-on single-shard fleets take no new pass at
all; and per-poll overhead after startup is a userspace no-op with no I/O.

## Observability Evidence:

No new signal. The pass is operator-visible through the pre-existing,
unconditional `deferred_backfill_completed evidence_facts=%d
readiness_rows=%d duration_s=%.2f batch_size=%d` log line (emitted once per
pass, asserted exactly-once by the composition test) and the existing
`DeferredBackfillDuration` / `DeferredBackfillEvidence` instruments. The
rebuild procedure doc now tells the operator to expect that line after
starting a collector-off ingester, and to restart an already-running
ingester after an out-of-band recovery so the startup pass fires after the
recovery.
