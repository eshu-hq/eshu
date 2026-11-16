# #10767: the NVD baseline trip sampler counts inflow, not survivors

Refs #10767, #10760 (F8), #10728, #10746. Part of #9209. Ruling:
[10728-baseline-ruling-2026-11-16.md](../arbiter/10728-baseline-ruling-2026-11-16.md)
decision 3. Design:
[10728-baseline-pacing.md](../design/10728-baseline-pacing.md) section 5.
Environment identifiers are redacted per #7803.

## Problem, re-verified against the code

- The winners maintainer drains `supply_chain_impact_winners_dirty_log` with
  `DELETE` (`deleteWinnersKeyedAppliedDirtySQL` and
  `deleteWinnersKeyedAllDirtySQL`). The old sampler ran
  `count(*) FILTER (WHERE advisory_id LIKE 'CVE-%' AND captured_at > $1)` over
  the log, so it counted only the rows that survived the drain. A maintainer
  that keeps up reads as quiet.
- Migration 272's shape constraint forces `advisory_id` NULL on
  `kind = 'scope'` rows, so no `LIKE 'CVE-%'` filter can see them.
- Reproduced red first (`nvd_baseline_trip_sample_live_test.go`, commit
  01e66d7c1d, run against a disposable PostgreSQL 18.6): a writer commits
  batches of CVE-keyed, GHSA-keyed and scope rows while the real keyed
  maintainer drains the log in 20-row chunks.

  ```
  concurrent sample counted 84 NVD-proxy rows, want between 91 and 91 committed
  --- FAIL: TestNVDBaselineTripSampleCountsInflowNotSurvivorsLive
  DirtyProxyRowsPerHour after a full-path drain = 0, want 7
  --- FAIL: TestNVDBaselineTripSampleCountsInflowOnTheFullRebuildPathLive
  ```

## Designs evaluated

| # | Design | Keeps the ruled CVE-% definition | Hot write path | Verdict |
|---|---|---|---|---|
| 1 | Sequence high-water delta carried between samples | No: a delta counts every source | none | Rejected |
| 2 | Capture-side counter | Yes | every capture statement, plus a hot row | Rejected |
| 3 | Drain-side tally written by the maintainer's DELETE | Yes | none | **Chosen** |
| 4 | Reconstruct from `fact_records` timestamps | Partly | none, but a scan of the biggest table | Rejected |

### 1. Sequence high-water delta

The log's `seq` is `BIGSERIAL`, so `pg_sequence_last_value` minus the value at
the previous sample is exact all-source inflow, rollbacks aside (they burn
values and over-count). It cannot apply the ruled filter, because the sequence
does not know a row's advisory prefix. The measured all-source inflow is the
disqualifier: M6 of
[10671-nvd-feed-measurements.md](10671-nvd-feed-measurements.md) records
15,647 (partial), 59,975, 80,753, 109,133, 95,266, 75,277 and 81,079 rows an
hour on QA, almost all GHSA-keyed (newest 50,000 rows: GHSA 49,561, CVE- 433).
The trip is more than 15k rows an hour for 2 consecutive samples, so an
all-source measure would sit tripped permanently with the baseline off. The
definition would have to change to a threshold against a different quantity,
which is the owner-level call the ruling reserves. It also needs state carried
between samples (memory is lost on a coordinator restart, a table collides
with the #10768 trip storage).

### 2. Capture-side counter

It would keep the filter, but it touches every statement that appends to the
log: `captureWinnersDirtyFactsSQL`, the two retract statements in
`writer_retract.go`, the audit queue, the scope-remainder conversion, and the
activation, scope-delete and scanner triggers (migrations 272 and 272a). A
counter row per hour or minute makes every writer transaction take the same row
lock until it commits. Migration 272 states the design rule it would break:
"writers never lock each other or any existing row".

Measured (throwaway shim, PostgreSQL 18.6, 8 clients, 15 s, each transaction
inserts 2,000 dirty rows, optionally upserts one minute-bucket counter row,
then spends 20 ms before commit as a stand-in for the rest of the writer
transaction; pgbench, shared busy machine):

| Variant | tps | mean latency |
|---|---|---|
| insert only | 230.2 | 34.8 ms |
| insert plus counter-row upsert | 45.9 | 174.5 ms |

The counter serializes writers behind the lock holder, about 5x the latency in
this shape. A statement trigger over the transition table does not change the
lock, since it still upserts the same bucket row. Rejected on the contention
cost alone, before counting the six statements it would edit.

### 3. Drain-side tally (chosen)

The maintainer's two DELETE statements already run
`WITH deleted AS (DELETE ... RETURNING source) SELECT source, count(*) ...`.
They now return `source, kind, captured_at, advisory_id, scope_id` and two
data-modifying CTEs in the same statement add the NVD-proxy and NVD scope rows
they delete to `supply_chain_impact_winners_dirty_tally`, keyed by capture
minute, and prune buckets older than three hours. The sampler reads, in one
statement (one snapshot), the rows still queued in the window plus the tally
buckets that overlap it.

- Exactness. The DELETE and the tally upsert commit together and the sampler's
  two subqueries share one snapshot, so a captured row is in the log until the
  DELETE commits and in the tally after, never both and never neither. The live
  proof samples concurrently with a draining maintainer and asserts every
  sample lies between the rows committed before and after it.
- Attribution. Buckets are the capture minute, so a row captured in the trailing
  hour counts toward that hour whenever the maintainer drains it. A 58-hour
  backlog the maintainer finally chews through is outside the two-hour tally
  horizon and writes nothing.
- Over-count only. Buckets that overlap the window edge add at most one minute
  of drained rows. Scope-remainder rows the maintainer re-queues are fresh
  captures and count again. Both over-count, the ruling's safe direction.
- Lock and deadlock analysis (concurrency-deadlock-rigor). The only writers are
  the two DELETE statements, which run inside the keyed tick under the
  single-flight resweep lock (`claimSupplyChainImpactWinnersResweep`, then
  `settleWinnersKeyedDirtyLog`) or the full-rebuild fallback in the same
  transaction. One writer at a time, so tally rows never contend, and the upsert
  (`bucket` in the last two hours) and the prune (`bucket` older than three
  hours) touch disjoint rows inside one statement. The sampler takes no lock.
  No capture transaction reads or writes the tally.
- Hot write path. No capture statement changes. A unit test pins that
  `captureWinnersDirtyFactsSQL`, the audit queue and the scope-remainder
  conversion do not name the tally; the retract statements, the triggers and
  the scanner triggers are untouched files.

### 4. Reconstruct from `fact_records`

The finding facts carry the advisory id, but the only supporting index is the
package or active-scan lookup, the retraction tombstones overwrite the change
time, and the probe would scan the largest table hourly. Not pursued.

## Cost on the maintainer (performance-rigor)

Throwaway shim: a 1,000,000-row copy of the log (60 hours of rows, 2 percent
CVE-keyed), the tick's real shape of one DELETE of 5,000 rows by `seq` (the
default `ChunkMaxDirtyRows`) with `RETURNING`, with and without the tally CTEs.
10 interleaved pairs each, PostgreSQL 18.6, a shared busy machine:

| Chunk of 5,000 rows | plain DELETE median / p90 | DELETE plus tally median / p90 |
|---|---|---|
| newest rows (tally writes about 18 buckets) | 4.5 / 5.6 ms | 5.9 / 7.4 ms |
| 58-hour-old rows (tally filtered out) | 5.7 / 7.0 ms | 6.4 / 7.6 ms |

That is about 1.4 ms (recent) and 0.7 ms (old) on a keyed tick whose other
statements (identifier build, probe, winner upsert) run in the hundreds of
milliseconds to seconds. The over-cap fallback deletes the whole 1,000,000-row
log in one statement: 1.07 s and 3.76 s plain, 1.45 s and 0.77 s with the
tally. That run is inside the machine's noise, so no delta is claimed for it.

The sampler's cost is unchanged in shape: one pass over the log (count, filter
and min together) plus a primary-key range scan of at most a few hundred tally
rows. The pass over the whole log existed before this change (there is no
index on `captured_at`); it runs hourly at coordinator cadence, off every hot
path.

## Scope rows

A scope row stands for the findings of one generation, so counting it as one
row under-represents it. Evidence from the code:

- NVD scope ids are `vuln-intel://nvd/...` (`vulnsource.NVDScopePrefix`), one
  scope per baseline chunk (`vuln-intel://nvd/_baseline/<run>/<year>/<start>`),
  so an NVD pointer flip is recognizable without an advisory id.
- The maintainer expands a scope row to the finding facts of that
  (scope, generation) (`buildWinnersKeyedScopeIdentsSQL`). A chunk's first
  activation precedes the reducer pass that writes its findings, so that fan-out
  is close to zero at flip time; the load arrives later as `fact_upsert` rows
  with CVE ids, which the ruled rate already counts. This is reasoning from the
  write order, not a measurement; the dirty-log rows per committed fact figure
  stays on the ruling's required disposable-DB proof.
- Fan-out is realized when the maintainer expands the row, so weighting a row by
  its fan-out at the capture hour is wrong for the hour, and a surviving row has
  no cheap fan-out figure (the expansion probes `fact_records`).
- A scope row that is only partly applied is converted to CVE-% fact rows, which
  the ruled rate counts; one that waits shows in the oldest-dirty age.

Decision: count NVD scope rows one per row, in a separate
`NVDScopeRowsPerHour` field beside the ruled `DirtyProxyRowsPerHour`, and do
not fold them into the 15k comparison. That keeps the ruled signal exactly as
ruled. The field is new, so the #10768 trip code that reads the sample keeps
compiling.

## Definition of the signal

The ruled definition holds. `DirtyProxyRowsPerHour` is the trailing hour's
`kind = 'fact'` rows with `advisory_id LIKE 'CVE-%'`, now counted at inflow.
Nothing about the threshold, the two-sample rule or the age trip changes.

## Open questions for the coordinator and the arbiter

1. Should NVD scope rows fold into the 15k comparison? Recommendation: no. At
   the ruled pacing of one baseline chunk per 30-minute planner run they add
   about two rows an hour against 15,000, and a weighted figure changes the unit
   from rows to expanded identifiers. If the owner wants the fan-out visible,
   measure it in the required disposable-DB run (the maintainer already builds
   `winners_keyed_scope_idents` at drain time) and weight in the drain tally,
   attributed to the drain hour, as a separate ruling.
2. Migration numbering. This change takes `298`. The #10768 branch adds trip
   storage and will want a migration too; whichever lands second renumbers, and
   `goldenBootstrapDefinitionsDigest`, the definitions count and the checksum
   manifest in `go/internal/storage/postgres/migrations` need recomputing.
3. Grants. The maintainer login needs `SELECT, INSERT, UPDATE, DELETE` on the
   tally and the coordinator login `SELECT` on the tally and the dirty log. The
   public telemetry reference now says so; the deployment's least-privilege
   grants (private IaC) are the coordinator's to confirm before #10746.

## Proof

| Check | Result |
|---|---|
| Red, then green | `TestNVDBaselineTripSampleCountsInflowNotSurvivorsLive` and `...OnTheFullRebuildPathLive`, red on the surviving-rows measure (above), green on the tally |
| Concurrency | `go test -count=6 -race -run CountsInflowNotSurvivorsLive`: pass; the sampler runs concurrently with the writer and a chunked maintainer |
| Window and retention | `TestNVDBaselineTripSampleWindowAndRetentionLive`: 90-minute and 58-hour rows excluded, the 58-hour row writes no tally row, a four-hour-old bucket is pruned |
| Maintainer unaffected | `go test -run TestSupplyChainImpactWinners ./internal/storage/postgres/` against the disposable database: 50 tests pass, none skipped or failed |
| Rollback atomicity | Pinned by one-statement construction and the concurrent bounds check, not by a forced mid-tick failure |
