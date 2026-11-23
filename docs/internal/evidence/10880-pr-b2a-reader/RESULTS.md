# #10880 PR-B2a reader: product timings (PostgreSQL 18.6, shared host, 8,000,481 supports)

Refs #10880. Environment: private cluster, `shared_buffers=2GB`, `work_mem=16MB`,
server `jit = on` (the V and image routes switch it off for their transaction),
host load 6 to 17 during the runs. Corpus: 80,000 canonical keys x 100 supports
plus 281 `key:big` supports and 200 operator rows, 15,000 keys on `pkg:npm/axios`,
37,923 winners precise, 21,936 candidate keys after the status prune, one hot image
(about 160,000 supports) and 20,000 ordinary images, the two B2a partial indexes
present. 3,000 repositories map to 300 services, 500 workloads and 3 environments
through seeded runtime facts, so the runtime-filter CTE and the grouped reads do
real work.

Performance Evidence: every shipped read finishes under the ruling's bar.

| Read (warm median, ms) | 200 B payloads | 6.25 KB payloads | Every winner fails precise (6.25 KB) |
| --- | --- | --- | --- |
| list precise, status, page of 201 | 225.7 | 238.1 | 520.2 |
| list precise, status, ascending sort | 224.8 | 238.3 | |
| list precise, status + min score 60 + severity | 178.6 | 195.5 | |
| list precise, priority bucket critical | 219.4 | 237.4 | 513.8 |
| list precise, suppression accepted_risk | 191.9 | 205.3 | |
| list precise, cursor | 203.2 | 220.2 | |
| list precise, one CVE | 22.6 | 23.9 | |
| list precise, axios (15,000 keys) | 54.3 | 67.7 | |
| list precise, axios + status | 64.0 | 77.0 | |
| list precise, repository | 26.0 | 27.7 | |
| list precise, service filter (runtime CTE) | 46.2 | 53.1 | |
| count precise, no selector | 226.0 | 237.0 | 516.2 |
| count precise, status | 227.5 | 234.8 | 515.2 |
| count precise, axios | 53.6 | 62.7 | |
| count precise, one CVE | 21.7 | 24.3 | |
| inventory precise, five dimensions | 195.8 to 207.4 | 207.6 to 221.6 | 495.9 (status) |
| grouped precise, service / workload / environment | 382.6 / 396.2 / 392.9 | 399.3 / 406.0 / 403.8 | 685.5 (service) |
| grouped precise, axios, three dimensions | 222.1 to 245.4 | 225.9 to 248.2 | |
| list image, typical (about 120 supports) | 8.5 | 12.9 | |
| count and inventory image, typical | 6.2 to 7.3 | 6.7 to 7.8 | |
| grouped image, typical, three dimensions | 228.0 to 241.2 | 227.3 to 241.8 | |
| jit-off scope (BEGIN, set_config, COMMIT), per 100 | 5.9 | 5.7 | |

The largest single reading is 685.5 ms (grouped, every winner failing precise);
the largest maximum is 703.0 ms. Over 40 reads, the slowest ordinary read is 406.0
ms. The jit-off scope costs 0.06 ms per read (5.9 ms per 100 scopes, before the lab I/O change; 9.5 ms per 100 after it, see the JIT section).

## Scale of the unscoped precise reads (route V), hot keys, and JIT

**Envelope.** Measured on the 8,000,481-support lab corpus: 80,001 canonical keys,
about 100 supports per key, 21,936 supplement candidates (64,001 when every winner fails
the precise filter). QA held 79,585 keys and 8,178,450 supports on 2026-11-21. List,
count and inventory read 226 to 247 ms warm, and 496 to 520 ms when every winner fails
the precise filter (grouped inventory 686 ms). A hot package (15,000 keys, 1,500,000
supports) reads 54 to 77 ms. A hot CVE on 20 % of supports (16,242 keys) reads 69 to
96 ms, max 136. That corpus is the proven envelope; nothing above it is proven.

**These reads are not O(page).** Each one scans every stored winners row twice and probes
one index entry per candidate key. On a 2,600,000-key corpus with one support per key
(the b2c hot corpus, copied read-only into a private cluster; half the keys precise,
CVE-HOT on 520,085 keys, `pkg:hot` on 1,600,000 keys) the same statements miss the
1,500 ms bar: 5,171 ms with no selector, 3,637 ms for a 1,600,000-key package, 1,937 ms
for a 520,085-key CVE. The count's grouping sort of `winner_groups` writes a 48,408 kB
temp file there (the statement is the shipped `aggregates_winners_queries.go` text, not
text this PR adds). The four list plans fit 1.12 s plus 1.56 us per matching key; every
read on that table pays the 1.12 s (two sequential scans of all 2,600,000 winners
rows), whatever the selector.

Derived, not measured: the no-selector read reaches 1,500 ms near 230,000 keys when every
winner fails precise at 100 supports per key, near 500,000 keys at the lab corpus's
candidate share, and near 750,000 keys when no probe finds a support. No fix ships here.
The limit, the QA census and the candidate fixes are recorded on #10880.

| Many-keys read (2,600,000 keys, one support each) | median | max | note |
| --- | --- | --- | --- |
| list, no selector, status | 5,170.9 ms | 5,206.0 | 1,300,999 rows through a top-N sort, 1,299,001 probes that all miss |
| list, package `pkg:hot` (1,600,000 keys) | 3,637.0 ms | 3,690.9 | |
| list, CVE-HOT and `pkg:hot` | 1,614.3 ms | 1,629.4 | |
| list, CVE-HOT (520,085 keys) | 1,936.9 ms | 6,055.9 (first run) | |
| count, no selector | 5,217.0 ms | 5,521.4 | grouping sort of `winner_groups` writes 48,408 kB (`temp read=6051 written=6056`) |
| count, package `pkg:hot` | 3,655.3 ms | 3,759.9 | no raw plan kept |
| inventory by status, no selector | 4,979.2 ms | 5,104.7 | |
| inventory by status, package `pkg:hot` | 3,536.1 ms | 3,595.1 | |

The lab associations and classifications tables are empty, so the anti-join probes on them
are excluded from these timings. The route-labelled
`eshu_dp_supply_chain_impact_read_duration_seconds` histogram is the operator signal for
this limit. The temp file is a decision-11 condition whose trigger is `work_mem`
(37 B per matching key), not the key count; it blocks B3, not this PR.

**Legacy.** No legacy baseline exists on either corpus: the 8M corpus holds supports and
winners but no finding facts, and the many-keys corpus's finding facts hold the unmutated
values (the update to the mutated values ran 52 minutes and was cancelled). The before and
after on real data is the D2 pair on the QA replica.

**JIT, measured through the Go stores (8M lab corpus, server `jit` on by default, wrapper
against the same store without it).**

Custom-plan executions (the first five executions of a statement on a connection, and
again after a connection is recycled; interleaved, n = 12, before the lab I/O change):
list with status 1663.5 ms without the wrapper and 287.6 ms with it (82.7 % saved); count
with status 1607.6 and 293.0 (81.8 %); count with no selector 1610.3 and 290.0 (82.0 %).
Without the wrapper these executions are over the 1,000 ms bar.

Generic-plan steady state (each arm on its own connection, `SetMaxOpenConns(1)`, so the
unwrapped arm's plan was built with `jit` on; 8 warm-ups per arm; the V statement's
`pg_prepared_statements.generic_plans` asserted >= 1 on the raw connection before
sampling; n = 25 interleaved; after the lab I/O change):

| Read | no wrapper | wrapper | net saving | highest `generic_plans` of any V statement on the raw connection |
| --- | --- | --- | --- | --- |
| list with status | 736.8 ms | 233.2 ms | 68.3 % | 3 |
| count with status | 229.8 ms | 231.2 ms | 0.6 % slower with the wrapper | 28 |
| count with no selector | 234.9 ms | 231.9 ms | 1.3 % | 36 |

The check before sampling reads the highest counter of any V statement on the raw
connection, and the list and the counts share that connection, so the 28 is the list
statement's counter (3 after its warm-ups plus 25 samples), not the count's. The count
statement's state follows from the counters: 36 is 3 after the count warm-ups, 25 samples
and 8 more warm-ups of the no-selector count, all on the generic plan. The per-execution
capture shows the same switch at execution 6.

A generic plan is compiled when it is built with `jit` on and costs more than
`jit_above_cost` (100,000). The list's generic plan costs 260,840 in this capture
(261,215 in the arbiter's earlier psql shim run) and runs with a JIT block (904 functions,
`Inlining false`, 679 to 698 ms). The two counts' shared generic plan costs 97,259 on this
corpus and is not compiled, only because it is under `jit_above_cost`: the margin is 2.7 %,
and a larger corpus or different statistics can put it over the threshold, after which the
counts would pay the compile in steady state without the wrapper. In the captured
executions the counts run 241 to 248 ms in steady state, within 1.3 % of the wrapped arm
(no state worse by more than 1 %); their first five executions on a connection are custom
plans with JIT (1.5 s, `Inlining true`), which the wrapper removes. The 20 % bar is read per
plan state: the list clears it in both states, the counts clear it in the custom-plan state
and are neutral in steady state. The plan-state capture is
`results_jit_plan_state_capture.txt`. D2 records the count statement's generic-plan cost on
the QA replica against its `jit_above_cost`.

The wrapper adds three round trips (BEGIN, `set_config`, COMMIT): 9.5 ms per 100 scopes
(0.095 ms each; median of three batches of 100) on localhost TCP with an idle
one-connection pool, through the plain `*sql.DB` path, each scope also running a
`SELECT 1`, after the lab I/O change (5.9 ms per 100 before it). It is not the cost through
the guarded read port (`BeginReadOnlySnapshot`, repeatable read). Not measured: at an
assumed 0.5 ms round trip the three add about 1.5 ms (arithmetic). Lab figures only: the QA reader
pool, `plan_cache_mode`, the JIT cost settings, any pooler and the round-trip time are not
measured; D2 records them and W1 reads the `pg_stat_statements` JIT counters for the two
routes.

**P1 plans (8M lab corpus, `auto_explain` analyze and buffers, V statements through the
product stores, after the lab I/O change).** Hot package axios has 3,557 supplement
candidate keys and the hot CVE has 4,416 (counted from the winners table with the same
predicate); the supplement probe node runs `loops=3557` and `loops=4416`, one per
candidate key, in every list, count and inventory plan (`p1_plan_*.txt`). No plan has a
node that loops over the active scopes, a `temp read` or `temp written` line, or an
external sort. The count with the hot CVE at `work_mem` = 4MB (`p1_plan_hotcve_count_wm4.txt`,
labelled) also writes no temp file at 4,416 candidates. Executions of the captured
connections: 53.9 to 63.2 ms in the plans kept, first runs up to 218 ms.

**Image explain with an identity (the public-id miss with an `image_ref`).** The image
explain statement with a non-empty `$2` on the largest seeded image (`registry.example/hot:1`,
100,219 supports; 8M lab corpus, after the lab I/O change; median of 5 after 1 warm-up).
Custom plan (a fresh connection each run, so each run includes connecting and planning):
205.4 ms with the fact id of a lower copy, 198.2 ms with the canonical key, 206.0 ms with the
fact id of the winner. Generic plan (`plan_cache_mode = force_generic_plan`, one connection):
70.3, 69.1 and 70.6 ms. All under the 1,000 ms bar. Every plan names
`supply_chain_impact_finding_supports_image_ref_idx` with no sequential scan of the supports
table (the generic plan from `EXPLAIN (GENERIC_PLAN)`). After a cluster restart (shared
buffers cold, OS cache warm) the first executions took 207.9 to 212.6 ms on the custom plan
and 94.8 to 105.4 ms on the generic plan. A cold OS cache was not measured. Raw lines:
`results_image_identity_arm.txt`.

## Limits

- The shim runs on a shared host at load 6 to 17, and the theory-proof JIT pair
  was measured at a different load; these rows are not comparable to the theory
  proof's `jit = on` rows, only to the bar.
- Payloads are padded text on the winners row and the best support of each key.
  Production width is not known; 6.25 KB is a stated stand-in, and the page read
  is 201 rows.
- The hot image (160,000 supports) was not re-run through the product statements.
  The theory proof sized it at about 50 to 85 ms per 10,000 supports; ruling
  decision 7 accepts it as a documented limit.
- The supports table here is current by construction (loaded directly, marker
  inserted), not written by the maintainer, and the winners watermark is set to
  now.

Observability Evidence: `eshu_dp_supply_chain_impact_read_route_total{route}` and the
`eshu.supply_chain.impact.read_route` span attribute name the read model of every
call; a refused call (list, count, inventory, grouped) counts the route it tried; the 503 envelopes name the model
(supports or winners) that is not ready.

**NOT_CHECKED:** QA candidate counts and skew, the QA replica latency and cold read,
real maintainer-written supports, concurrent writers during a read, the hot image
through the product statements, production payload width, the supplement candidate
count on the span.
