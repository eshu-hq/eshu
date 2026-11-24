# PR-C1k: the stable_fact_key fill (#10880)

PR-C stops writing the finding facts, so the writer's stored read must not need the
fact twin's key. Supports written before migration 350, and supports a rewrite has not
touched since, have a NULL `stable_fact_key`; the stored read falls back to the twin for
them (a primary-key subquery, evaluated only for a NULL key). The arbiter ruling on C1
(sections 1.4 to 1.7) requires a separate, throttled fill that finishes on QA before the
fact twin can go. This change builds it. It runs nothing by default.

## What it does

`SupplyChainImpactStableKeyFill` (`go/internal/storage/postgres/supply_chain_impact_stable_key_fill*.go`)
is the supports backfill's twin:

- **Marker.** A second row, `supply_chain_impact_stable_key_fill_v1`, in the existing
  `supply_chain_impact_finding_supports_backfill` table. Its `contract_version` is a
  separate constant that a test pins to at most the supports contract version: the
  readiness predicate and the readers refuse on any marker row above their own version
  (`backfill.go` "contract_version > $2"), so a fill marker above it would turn every
  supports reader to 503. The marker statements (ensure, read, advance, restart,
  verification cursor) are the backfill's, which act on a marker by name; a test proves
  the fill binds only its own marker name.
- **Walk and chunk.** Pairs in `(scope_id, generation_id)` order through the supports
  table's own index; per pair, keyless supports after the cursor in fact id order, at most
  500 per chunk (a hard ceiling, whatever size a caller passes). Each chunk is one
  transaction: the pair's conflict-domain advisory lock first (the lock the impact writer
  holds around its support writes), then
  `UPDATE ... SET stable_fact_key = f.stable_fact_key FROM fact_records f WHERE s.fact_id = ANY($1) AND s.stable_fact_key IS NULL AND f.fact_id = s.fact_id AND f.stable_fact_key <> ''`,
  then the cursor advance. It never overwrites a key (a rewrite may have filled it) and
  skips a twin with an empty key. It is never one statement over the table.
- **Completion.** A sliced verification (20,000 supports a slice) counts keyless supports
  whose twin has a key. A hit restarts the sweep; a clean slice advances the verification
  cursor; the clean last slice sets the marker. A keyless support whose twin has no key
  does not block completion (the reader treats it as missing either way).
- **One runner.** The reducer elects one owner through a session advisory lock (the
  backfill's election, generalized to take a lock id; the fill uses its own id), 200 steps
  or one minute a pass.

## Pace and pauses

- **WAL pace (default 8 GB an hour).** After each step the runner waits
  `rows filled x ESHU_..._WAL_BYTES_PER_ROW / budget`. The figure is an estimate, not a
  log-position delta: `pg_current_wal_lsn()` moves with every writer on a primary that
  already writes tens of GB an hour, so a delta around the fill's own transaction would
  throttle it for work it did not do. The per-row estimate (default 1,024 bytes) is what
  the lab run calibrates. At the defaults a 500-row chunk waits about 230 ms.
- **Pauses**, checked before every step: replica replay lag over 60 s
  (`pg_stat_replication`, which needs the `pg_monitor` role), free disk under 100 GB, and any
  index build (`pg_stat_progress_create_index` not empty). A condition that cannot be read
  pauses the pass (`probe_error`), never counts as clear.
- **Free disk cannot be read through SQL** on a managed server. It is a seam
  (`StableKeyFillProbe.FreeDisk`); with a limit set and no reading wired the fill pauses with
  `disk_unreadable`. An operator who watches disk elsewhere sets
  `ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_MIN_FREE_DISK_BYTES=0`. This is a gap against the
  ruling's "free disk under 100 GB" wording and is stated, not hidden.
- **Default off.** `ESHU_SUPPLY_CHAIN_IMPACT_STABLE_KEY_FILL_ENABLED` is false unless set: the
  fill must not run inside W1b, W2 or a counted window, or before the lab run.

## Hermetic proof

An in-memory supports table and twin keys drive the real `RunStep` (`supply_chain_impact_stable_key_fill_test.go`):
fills only keyless supports and never overwrites; chunks of at most 500 each under the pair
lock with the cursor advance in the same transaction; resume after a stored cursor; completion
only after a clean verification (a gap restarts; three slices advance, advance, complete); a
complete marker is inert; the marker's contract version and name. The pause matrix, the pace
arithmetic, the probe statements (no log-position read), the runner's pause-before-step and
pacing, the default-off start and the config defaults are tested too. Mutations each fail a
named test: removing the pair lock, removing the cursor advance from the chunk transaction,
removing the 500 ceiling, raising the marker's contract version, sharing the supports marker row,
and completing on a non-last slice.

## Owed to a lab or QA slot

Performance Evidence: PENDING. T6 of the ruling (fill 8M supports in the lab; report WAL bytes
per row, HOT share, duration, dead tuples) has not run, so the 1,024-byte per-row default and the
8 GB an hour pace are unmeasured. The QA projection above 100 GB of WAL returns to the arbiter
before any QA run. The live proof that the real statements run (the update, the verify slice, the
probe reads) and the real-row equality of a filled key with the twin's are owed to a slot.
No-Regression Evidence: nothing in the writer, the stored read or any reader changes; the
fill is off by default; one chunk holds one pair's lock for at most 500 rows. Observability
Evidence: `eshu_dp_supply_chain_impact_stable_key_fill_steps_total`, `..._rows_total`,
`..._pauses_total` and the `..._complete` gauge, documented in
`docs/public/reference/telemetry/supply-chain-impact-supports.md`; four new environment
variables in the env registry.

## Live and lab proofs to run

Written, not yet run. Environment, for a disposable server only: `ESHU_POSTGRES_TEST_DSN=<dsn>`,
`ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN=<admin dsn of the same server>`,
`ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE=1`, `ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF=1`. Command, from `go/`:
`go test -count=1 -v -run '<name>' ./internal/storage/postgres/`.

- `TestSupplyChainImpactSupportsStableKeyFillLive`: a 1,300-support pair; filled keys equal the twin's, a key a
  rewrite filled is kept, an empty twin key stays keyless, no step touches more than 500, the marker is set only by
  the step that verified clean, the supports marker row is untouched.
- `TestSupplyChainImpactSupportsStableKeyFillRespectsThePairLockLive`: with the pair's conflict-domain lock held, a fill
  step and a writer pass both wait (seen in `pg_stat_activity`), nothing is keyed meanwhile, and both finish with no lost
  update.
- `TestSupplyChainImpactSupportsStableKeyFillResumesAfterARestartLive`: one step, then a new fill value over the same
  database finishes the sweep; every fillable support is filled exactly once.
- `TestSupplyChainImpactSupportsStableKeyFillLabLive` (T6): `ESHU_LAB_FILL_ROWS` (default 5,000) and
  `ESHU_LAB_FILL_PAIR_ROWS` (default 2,000) set the scale; it seeds keyless supports across pairs, runs the fill to completion
  at the production chunk size, and reports WAL bytes per filled row (log position delta and `pg_stat_wal`), the HOT share,
  dead tuples, wall time and the per-step time. `ESHU_LAB_FILL_REPORT=<path>` writes the JSON. Run it on a quiet server: the WAL
  deltas include every writer.
- `TestSupplyChainImpactSupportsLazyStoredReadPlanLive`: `EXPLAIN (ANALYZE, BUFFERS)` of the stored read: for a keyed pair the twin
  SubPlan is never executed and `fact_records_pkey` is not probed; for a keyless pair it is probed once per row; both answers equal
  the fact read. `ESHU_LAB_PLAN_DIR=<dir>` writes the two plans.
