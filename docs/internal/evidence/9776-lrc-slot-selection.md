# OSV owned-package slot selection by least recently planned (#9776)

With `derive_from_owned_packages.slot_progress: true` the reader picks the V/P
least recently planned virtual slots (V = 1,024) and the coordinator records
them in `owned_target_slot_progress` when the work is admitted. A reconcile
bucket the coordinator never ran no longer skips its slot for a whole rotation.
The default stays the wall-clock slot, and the flag-off statement is
byte-identical to the shipped one (sha256 pinned in
`owned_package_targets_slot_progress_test.go`).

Performance Evidence: the joined reader statement was measured on a private
native PostgreSQL 18.6 (no Docker, no shared database, no QA) against a fixture
rebuilt from the description in the #9426 evidence note, not from the #9483
generator, so "QA-shaped" holds by description only: 3,314 Git scopes with 1 to 41 generations each, 2,490,432 dependency
fact rows (225,352 in active generations), about 32.7k distinct version-specific
targets, `VACUUM ANALYZE` done. The statement text under test is the one the Go
builder ships (`listOwnedPackageDependencySlotProgressQuery`), dumped from the
tree, not a shim. Each scenario seeds progress so the picked slots equal the
clock slot, runs one untimed warm-up, then three `EXPLAIN (ANALYZE, BUFFERS)`
runs per statement, interleaved with the first mover alternating:

| Scenario | L | P | Picked slots | Old buffers | New buffers | Delta | Fact-scan node | Rows (EXCEPT both ways) |
| --- | ---: | ---: | --- | ---: | ---: | ---: | --- | --- |
| A | 100 | 1,024 | 1 (V/P = 1) | 1,584,419 | 1,584,079 | -0.02% | unchanged | 0 and 0, 35 rows |
| B | 1,500 | 64 | 16 | 1,584,419 | 1,584,077 | -0.02% | unchanged | 0 and 0, 507 rows |
| C | 10 | 8,192 | P > V, `clock_fallback` | 1,584,419 | 1,584,059 | -0.02% | unchanged | 0 and 0, 8 rows |

Buffers were identical across the three runs of each statement. Wall times were
1.76 to 1.97 s on both statements, but the host load average was 32 to 42 while
they ran, so wall time is not claimed; the buffer count is the deterministic
figure. The fact-scan node in every plan, old and new, is an index scan on
`fact_records_content_entity_dependency_variable_repo_idx` under the
active-generation InitPlan. The new statement adds a read of at most 1,024
progress rows for one instance, through the primary key. An earlier shim with a
different slot-pick formulation gave the same result (deltas within 0.02%); the
shipped text was then re-measured as above.

The joined-statement table above was measured before the second review moved the
progress key from an interval index to the window start time. That change edits
only the progress subplans: the fact-scan head is byte-identical, and the
`$8` window-start bind replaces `$3` in the reuse predicate. Those subplans were
re-run on a private PostgreSQL 18.6 against a table of 2 x 1,024 rows (two
instances, one filtered): the reuse lookup is an index-only scan on
`owned_target_slot_progress_oldest_idx`, 6 buffers, 0.042 ms; the oldest-slot
pick is a 1,024-row hash join with a top-N sort, 18 buffers, 0.413 ms; the
MIN and COUNT stats are one 18-buffer scan, 0.177 ms. The 1,584,xxx-buffer
joined figures were not re-run on the new text (NOT_CHECKED).

No-Regression Evidence: the flag-off path runs the unchanged statement. The
`slot_progress` path adds one progress read inside the existing statement (above)
and one extra compare-and-set `INSERT ... ON CONFLICT` of at most V/P rows inside
the admission transaction that already exists, or one small advance-only
transaction when the pick holds no work. Planning cadence is unchanged: one read
and one admission per instance per reconcile tick.

## Scale

The slot count P follows the target total T and the target limit L:
P = next_pow2(ceil(2T / L)). The 1x rows were measured: Scenario A at L = 100
(P = 1,024) and Scenario B at L = 1,500 (P = 64, 16 slots). Scenario C measured
the statement shape of the P above V fallback. The 5x and 10x rows are INFERRED
from the formula, not measured.

| Scale | T | P at L = 100 | Slots per window | P at L = 1,500 | Slots per window |
| --- | ---: | ---: | --- | ---: | ---: |
| 1x | 32,726 | 1,024 | 1, progress | 64 | 16, progress |
| 5x | 163,630 | 4,096 | 0, **clock fallback** | 256 | 4, progress |
| 10x | 327,260 | 8,192 | 0, **clock fallback** | 512 | 2, progress |

Progress selection holds while P is at most V = 1,024, that is while T is at
most 512 times L. Above that, a window cannot hold whole virtual slots, so the
read keeps the clock slot, advances nothing, and warns
`owned_target_progress_clock_fallback`; operators see
`eshu_dp_owned_target_slot_selection_total{outcome="clock_fallback"}`. At L = 100
the 5x and 10x cases fall back; raising L or shrinking T restores progress
order. The progress table holds at most 1,024 rows per instance (10,240 rows for
ten instances) whatever T is. This change does not shorten the rotation:
the rotation length is P times the reconcile interval, and #9222 is the
prerequisite for progress order to matter at 5x and 10x.

## Concurrency

Conflict domain: one `(collector_kind, collector_instance_id)` pair. Progress rows
change only under the existing transaction-scoped `pg_advisory_xact_lock` keyed by
the FNV of that pair, the same lock `CreateRunWithWorkItemsIfNoOpenTargets`
takes. The advance is an upsert compare-and-set
(`WHERE last_planned_window_start < EXCLUDED.last_planned_window_start`), so a
coordinator whose clock is one window behind moves zero rows and can never
regress a slot.
`RowsAffected` zero for a requested move is not an error. The coordinator counts
it as `lost_race` only for a fresh pick (`oldest`); a repeat tick in the same
bucket is `same_bucket_reuse`, because the ticker runs many times per bucket.

Paths that commit run the advance in the same transaction: the insert, the
terminal-run skip and the zero-eligible skip. A rolled-back pass moves nothing.
A pick with no work items (a virtual slot with no targets) takes an advance-only
transaction under the same lock key and retry budget; without it the same empty
slot stays oldest at every tick and the rotation stalls. Lock order: the advisory
lock first, then the progress rows. No path takes a second lock, so there is no
ordering cycle to deadlock on. A repeat reconcile inside one window finds rows
already at that window start and reuses exactly those slots
(`same_bucket_reuse`), so the plan key and run id, which derive from the
truncated clock, reproduce the same plan. Every row such a tick picked is
already at this window start, so the compare-and-set could only move zero rows;
the coordinator builds no advance for it, which spares the row locks and, for an
empty pick, an advisory-locked transaction on each of the ticker's repeats.

Changing `scan_interval`: the key is the window start time in unix seconds
(`last_planned_window_start`), not a count of scan intervals. The first design
stored unix time divided by the interval, so doubling the interval halved every
new key below every stored one: the compare-and-set moved nothing, the same V/P
slots were planned forever, and the age gauge read zero (found in review 2).
Halving the interval doubled the keys and reported a 28 year old slot. With a
time key, a raise leaves the rows written in the last old-width window up to
one new window ahead of the new window start, so those slots wait one window;
a lower leaves nothing looking old. No migration is needed when an operator
changes the interval.

Truncation: the final `ORDER BY` is `(virtual_slot, target_rank)`, so a read cut
by its limit drops whole trailing slots. The advance covers the picked slots
strictly below the last returned slot. The last returned slot advances only when
no row-bearing slot precedes it (the head slot alone exceeds the limit), and then
`head_truncated` is set so an oversize head cannot pin the instance.

Proof, on the private cluster (`TestOwnedTargetSlotProgressLive` and
`TestOwnedTargetSlotProgressSurvivesPFlipLive`, exit 0):

- A bucket never read leaves the clock statement with missed targets (planted
  RED), while progress order plans all 2,319 targets within one rotation.
- A repeat reconcile reproduces the plan, and the second advance moves no row (`same_bucket_reuse`).
- A deleted progress row is picked first; an empty slot and a failed work item
  both advance so neither pins the head.
- An oversize head advances with `head_truncated`; a partial later slot does not.
- P above V returns the clock statement's rows with no progress written.
- A P flip in the middle of a rotation keeps planned slots out of the next pick.
- Two concurrent coordinators move each slot exactly once over ten iterations,
  and a stale bucket moves nothing.
- Raising `scan_interval` from 30 minutes to 1 hour after 40 warm windows keeps
  the rotation on the oldest group each window with every row moved, and the
  first new window reads a 16 hour age (`TestOwnedTargetSlotProgressSurvivesScanIntervalChangeLive`,
  RED on the interval-index key: 0 of 32 rows moved). Lowering 1 hour to 30
  minutes reads the true 32 hour age (RED: 249,144 hours).
- Seeded violation: removing the compare-and-set from each of the three commit
  paths turned the per-path guard test (`slot_progress_advance_test.go`)
  RED, and restoring it turned it GREEN.

Observability Evidence: `eshu_dp_owned_target_oldest_slot_age_seconds` (gauge,
`collector_instance_id`; reported once all 1,024 slots have a row and the read is
not in fallback) and `eshu_dp_owned_target_slot_selection_total` (counter,
`collector_instance_id`, `outcome` from the closed set `oldest`,
`same_bucket_reuse`, `clock_fallback`, `empty_slot`, `truncated`, `advanced`,
`lost_race`). One INFO line, `workflow coordinator advanced owned package slot
progress`, carries `selection_mode`, `virtual_slots`, `slots_requested`,
`slots_advanced` and `outcomes`. Two WARN reasons,
`owned_target_progress_clock_fallback` and
`owned_target_slot_age_exceeds_max_staleness`, name the two states an operator
acts on. The window log line adds `selection_mode`, `virtual_slots` and
`oldest_slot_age_seconds`; the `owned_package_targets.read` span adds
`selection_mode`, `selected_virtual_slots`, `oldest_slot_age_seconds` and
`head_truncated`. At 3 AM: age rising with `advanced` flat means the coordinator
stopped planning; `lost_race` rising with `advanced` flat means fresh picks
find the bucket already recorded, so another coordinator is doing the work; `clock_fallback` means the flag is on but not in
effect. The age counts planned windows, not collected ones. Reference:
`docs/public/reference/telemetry/owned-target-slot-progress.md`.

## Why the miss matters

VERIFIED 43 readable buckets across two days in the QA environment, 0 missed;
40 of 48 buckets in the 24 h window are NOT_CHECKED, including the one pod roll
in that window. The measured miss rate is therefore zero where it could be read,
so this change closes a gap that is real by construction (a clock slot cannot be
replayed) and not yet seen in the QA environment. Its priority stays below #9222
and #9426.

## Migration number

The table is migration 271, and its column is `last_planned_window_start`. Number 270 is held by an unmerged sibling migration,
so this change uses the next free number. If 270 lands first, rebase and
re-derive the embedded count, the digest in `embed_invariant_test.go` and the
manifest row in `migration_checksum_manifest_test.go`. Never edit an applied
migration file.

## Not checked

- Live QA gauge values: the flag is off by default and has not been turned on in
  a shared environment.
- The joined-statement buffers on the post-review text (only the progress
  subplans were re-measured; see Performance Evidence).
- `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline` needs
  `ESHU_DEPLOY_CHECKOUT` and fails the same way without this change.
