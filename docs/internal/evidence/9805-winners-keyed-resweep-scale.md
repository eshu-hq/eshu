# #9805 keyed resweep: scale, plan stability and writer cost

Refs #9805. Companion to [9805-winners-keyed-resweep.md](9805-winners-keyed-resweep.md),
which holds the design, the capture owners, the concurrency argument, the
built-binary proof and the markers.

## Scale

Harness: `TestSupplyChainImpactWinnersResweepScaleProofLive`
(`supply_chain_impact_winners_keyed_scale_live_test.go`). It builds the corpus
through the production impact writer, runs the full resweep, rewrites 1% of the
findings through the writer, times one keyed tick and one full resweep over the
same state, requires the keyed table to equal the full recompute, and with
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_SCALE_EXPLAIN=1` attaches the
`EXPLAIN (ANALYZE, BUFFERS)` plans of the tick's `active_facts` build and apply
statement. Knob: `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_SCALE_PROOF=smoke|1x|5x|10x`
(unset is smoke, two percent of 1x, so it runs in the gate). Corpus: 1x is
100,000 findings in 100 scopes of 1,000, key = finding mod 5N/6 (about 1.2
candidates per key), ten keys per package triple, one key in twenty on a single
hot triple (4,167 keys, about 5,000 candidates at 1x).

### Finding 1: the read half missed its bar because of the hot triple

Before the change, 1x, one scope (1,000 identifiers, 1%) rewritten:

| Run | Host load1 | Keyed tick | Full resweep | Full / keyed |
| --- | --- | --- | --- | --- |
| 1 (09:38) | 8.9 to 14.4 | 1.099 s | 5.896 s | 5.4x |
| 2 (09:41, EXPLAIN) | 20 to 15 | 1.068 s | 5.737 s | 5.4x |

The bar is full / keyed of at least 10x. The plan of the `active_facts` build
(run 2, 983.6 ms of the 1,068 ms tick; the apply statement is 161 ms) is an
`Index Scan using fact_records_supply_chain_impact_package_lookup_idx`, 1,000
loops, 0.959 ms each, 2 rows returned per loop, **Rows Removed by Filter 266
per loop (about 266,000 heap rows fetched and thrown away)**, 276,414 shared
buffer hits. The index is keyed on the three payload columns package_id,
repository_id and subject_digest only, so every identifier fetches its whole
triple and the c0 and advisory_id comparisons can only filter after the heap
fetch. The churned scope holds 50 identifiers on the hot triple (50 x about
5,000 = 250,000) and 950 on ordinary triples (950 x about 11 sibling rows =
10,500): 260,500 expected, 266,000 measured.

### Finding 2: the statement's plan was unstable

In runs 3 and 4 (load 25 to 60) the same statement ran with a different plan:
`scope_generations` outside, the per-identifier lateral probe inside it, 101
loops of 1,000 probes (`loops=101000`), 28.4 million shared buffer hits for a
2,000-row result. The keyed tick took 2 m 32 s and 2 m 38 s, and `EXPLAIN
(ANALYZE)` of the statement 164 s, 247 s, 472 s and 720 s. The 100-fold buffer
count is the plan, not host load. The planner estimated the lateral at one row,
so any join order looked cheap; the small scope tables of the isolated test
schema had default statistics. A deployed table is analyzed, but the statement
had nothing that held the probe to one execution per triple.

### The change and its proof

`buildWinnersKeyedActiveFactsSQL` now probes each DISTINCT
(package_id, repository_id, subject_digest) triple once, in a `MATERIALIZED`
CTE, then joins the identifiers on c0 and advisory_id and the scope tables. The
first candidate, without the CTE, was re-merged by the planner (the triples
joined back to the identifiers and the lateral ran 1,000 times, 266,150 rows
removed by the join filter): it gained nothing, which is why the CTE is
load-bearing.

Run 4, one 1x corpus, statements run back to back in one transaction:

| Statement | Buffers (shared hit) | Rows out | Execution |
| --- | --- | --- | --- |
| production form (flipped plan) | 28,365,188 | 2,000 | 472,240 ms |
| distinct-triple form | 15,539 (7,540 in the 101 index searches) | 2,000 | 189.98 ms |

Row-set diff of the two temp tables: only_in_production = 0,
only_in_candidate = 0. Against the good plan of run 2 (983.6 ms, 284,409
buffers) the new form is 5.2x faster and uses 18x fewer buffers. The load of
runs 3 and 4 makes the millisecond figures unreliable; the buffer and row
counts are the evidence.

Proof in the repository: `TestSupplyChainImpactWinnersResweepKeyedProbePlanLive`
seeds a 3,000-candidate hot triple with 30 dirty identifiers and asserts the
lookup index is searched once per distinct triple (21 searches for 50
identifiers). It is green on the new statement and red on the old form (100
searches, want 21). The parity test gains a case with three dirty keys on the
hot triple and one on another triple in a single tick; the keyed table equals a
fresh recompute.

### The remaining floor

One dirty key on a hot triple still fetches that triple's candidates once.
QA read replica, counted through the lookup index (relayed by the lead, the
largest triple itself not counted): the top five packages hold 9,724, 6,247,
5,121, 2,701 and 1,560 candidate rows (25,353 in total), with 115,000 to
134,000 candidates overall. The sampled `pg_stats` share (one distinct
subject_digest) is weak evidence that a triple is about a (package, repository)
pair. So the floor on the hottest package is 5,000 to 10,000 fetches for one
dirty key (about 20 ms at the 1x rate), and the rows sit in the CTE tuplestore,
which spills to a temp file past `work_mem` (10.9 MB at 6,901 rows). The
distinct-triple form removes the multiplication by the number of dirty keys on
that triple; it does not remove the first fetch.

### Before and after at 1x, 5x and 10x

One scope in a hundred rewritten (1,000 / 5,000 / 10,000 identifiers), one keyed
tick, one full resweep over the same state, `path=keyed` on every after-run. The
host was never quiet: load1 ran 10 to 124 across these runs because other lanes
were building and running live suites, so every millisecond below is
load-affected and no run met the load1 < 9 rule. The claim rests on the
load-independent columns: shared buffers (hit + read), index searches, rows
fetched, and the row-set diff.

| Scale | Form | Keyed tick | Full resweep | Full / keyed | `active_facts` build | Shared buffers | Index searches | Rows fetched / kept |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1x | before, good plan (run 2) | 1.068 s | 5.737 s | 5.4x | 983.6 ms | 284,409 | 1,000 | about 268,000 / 2,000 |
| 1x | before, flipped plan (run 4) | 2 m 38.6 s | 11.37 s | 0.1x | 472 s to 720 s | 28,365,188 | 101,000 | about 28 M probes / 2,000 |
| 1x | after (a1x) | 358.9 ms | 5.640 s | 15.7x | 132.7 ms | 16,435 | 101 | 6,901 / 2,000 |
| 5x | before, capped | over 600 s: statement canceled at the 10-minute cap (lower bound) | not reached | not reached | not reached | not reached | not reached | not reached |
| 5x | after (a5x) | 6.152 s | 60.81 s | 9.9x | 2,739.7 ms | 57,335 | 501 | 30,701 / 6,000 |
| 10x | before (b10x) | 3 m 51.2 s | 3 m 0.5 s | 0.8x | not captured (no EXPLAIN) | not captured | not captured | not captured |
| 10x | after (a10x) | 2.942 s | 1 m 16.5 s | 26.0x | 1,077.1 ms | 118,552 | 1,002 | 62,369 / 13,000 |

Loads: a1x 26 to 18; a5x 17, then 69 to 92 during the keyed tick and the
canary (the 5x ratio of 9.9x is not a clean read and is not claimed as a pass
or a miss); a10x 60 to 21; b10x 21, with samples up to 124. The control
canary (3M-row md5 query, five runs) slowed 2x to 4x between its before and
after readings on a5x and b10x, which is the reason ms are not used.

The 10x before-run did not reach the cap: the keyed tick took 231 s (23.1 ms
per identifier) against 2.94 s after, 79x, both under load. The 5x before-run
was canceled by `statement_timeout = 600000` in the `active_facts` build; a
watchdog was armed and `pg_stat_activity` showed no `winners_keyed` statement
left afterwards (0 in both runs). The good-plan form at 5x and 10x is
`NOT_CHECKED`: forcing the join order needs a plan pin that the old statement
does not have, and measuring it would not change the finding.

Rows fetched grow with the corpus because the synthetic hot triple holds one key
in twenty of all keys: about 5,000 candidates at 1x, 25,000 at 5x, 50,000 at 10x
(probe fetched 62,369 rows at 10x, 50,000 of them from the hot triple). The QA
replica's hottest package holds 9,724 candidates, so the 10x synthetic triple is
five times larger than the largest counted QA triple.

### Plan stability

`TestSupplyChainImpactWinnersResweepKeyedProbePlanLive` runs the statement under
`EXPLAIN (ANALYZE)` in two statistics states on one fixture (20 ordinary
triples, one hot triple of 3,000 candidates, 50 identifiers):

| Statement | Default scope-table statistics | After `ANALYZE` of the scope tables |
| --- | --- | --- |
| old per-identifier form | 100 index searches (the scope loop) | 50 index searches (one per identifier) |
| distinct-triple `MATERIALIZED` form | 21 | 21, with the `CTE triple_facts` node |

The `MATERIALIZED` CTE removes the join-order choice for the probe: it is
planned and run as its own subplan, once per distinct triple. What stays with
the planner is everything after it: the hash join of the materialized rows to
the identifiers (on five columns), and the two index lookups into
`ingestion_scopes` and `scope_generations`, both over the already-small probe
output (2,000 to 13,000 rows). Those cannot multiply the fact_records work.

The earlier measurements, from a harness that was deleted and cannot be re-run
(read half 11.5x / 14.8x / 9.5x faster at 1x / 3x / 10x; 155 / 117 / 203 us per
identifier), are NOT COMPARABLE with these: that corpus had no hot triple, 20
scopes, and no source hash binds it. They are kept only as the reason the first
estimate was optimistic.

## Writer cost

Method (H4): the same scale test with
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_WRITER_COST_ROUNDS=5`. Each round writes one
1,000-finding scope per arm through the production writer, one finding in a
hundred carrying a new priority (about 1% changed, the rest digest-unchanged).
The capture arm uses the production transaction and times the capture
statement; the control arm hides the capturer, so no capture statement is
issued. The arms alternate which goes first and swap scopes every round. The
0% case issues no capture statement by construction
(`TestWriteCapturesWinnerIdentifiersOnlyForNewOrChangedRows`).

Run 1 (1x, 09:38 to 09:39, load1 8.9 to 14.8, NOISY by the 12-for-two-samples
rule), per round, whole pass with capture / without, in ms:
296.3 / 228.7 (capture first), 202.6 / 190.5, 192.0 / 183.0, 157.9 / 164.7,
158.7 / 157.6. Median pass +4.9%. The capture statement alone: 0.27, 0.52, 0.19,
0.20, 0.17 ms, median 0.20 ms, 0.11% of the pass. Each capture arm appended 10
rows (50 in total). The whole-pass delta is inside the noise (the first rounds
ran at the highest load and fall toward zero as load fell), so it is not a
pass or a miss by itself; the direct cost of the INSERT, measured on its own,
is far under the +3% bar. The earlier whole-pass figure (+7.8% at 1% changed)
stays `NOT_CHECKED`.

After-change rerun (a1x, 10:43 to 10:44, load1 26 to 17, NOT clean by the
load1 < 9 rule; canary 1.114 s before, 1.151 s after). The production capture
statement is unchanged by the read-half SQL rewrite; this rerun confirms it on
the final tree. Per round, whole pass with capture / without, in ms: 226.1 /
191.8 (capture first), 152.8 / 152.8, 154.7 / 146.4, 140.2 / 143.8, 148.6 /
153.1. Median pass 152.8 / 152.8, delta +0.1%. Capture statement alone: 0.20,
0.21, 0.16, 0.18, 0.16 ms, median 0.18 ms, 0.12% of the capture pass; 50 dirty
rows appended in total (10 per round). The only round with a visible gap is the
first one, which also ran first and cold. The direct INSERT cost is under the
+3% bar by two orders of magnitude; the whole-pass delta is inside the noise.

## Scope delete trigger cost

The `BEFORE DELETE` trigger on `ingestion_scopes` (review finding F1) reads the
deleted scope's non-tombstoned findings through
`fact_records_collector_status_active_idx` (one index search) and appends one
log row per distinct identifier. The scale test measures it with
`ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_PURGE_FINDINGS=<n>`: it writes a scope of n
findings over the 1x corpus, runs `EXPLAIN (ANALYZE, BUFFERS)` of the trigger's
`INSERT ... SELECT` and of the purge's `DELETE FROM ingestion_scopes`, and
rolls back. Scope sizes of 1,000, 5,000 and 10,000 findings stand for 1x, 5x and
10x of the scale scope. Load1 was 38, 23 and 46, so the milliseconds are
load-affected; buffers and rows are not.

| Scope findings | Log rows appended | Capture `INSERT ... SELECT` shared buffers (index scan part) | Trigger time inside the DELETE | Whole DELETE |
| --- | --- | --- | --- | --- |
| 1,000 | 1,000 | 4,285 (254) | 16.8 ms | 107.2 ms |
| 5,000 | 5,000 | 21,414 (1,256) | 74.0 ms | 110.2 ms |
| 10,000 | 10,000 | 42,821 (2,509) | 581.4 ms | 863.6 ms |

The read is linear in the scope's findings, one index search at every size, and
the log write dominates the buffers. It runs once per deleted scope, in the
retirement purge, next to the cascade the same statement already runs. A scope
past the keyed cap (50,000 identifiers) sends the next tick down the full path,
which is the cost the old design paid on every tick.
