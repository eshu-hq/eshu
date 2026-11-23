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
ms. The jit-off scope costs 0.06 ms per read.

## Hot-key probe (arbiter-hotkey-page.md decision 11) and the JIT 20 % rule

Both ran through the real stores on private PostgreSQL 18.6 clusters (JIT default on,
the two B2a partial indexes present, supports marker and fresh winners watermark set).

**Supports-defined hot keys (the 8,000,481-support corpus, about 100 supports per key).**
A hot package with 1,500,000 supports is `pkg:npm/axios` (15,000 keys); a hot CVE on 20 %
of the table is 16,242 keys holding 1,624,200 supports (winners rows tagged; the V
statements never read `supports.cve_id`). Warm medians, p95 = max of eight runs:

| Read | hot package (axios) | hot CVE (20 % of supports) |
| --- | --- | --- |
| list, page of 201 | 54.3 to 77.0 ms | 96.4 ms (p95 136.3) |
| count | 53.6 to 62.7 ms | 70.1 ms (p95 80.3) |
| inventory by status | 195.8 ms for the unselected read | 68.7 ms (p95 79.4) |

All under the 1,500 ms bar; the plans have no per-scope probe loop and no sort on disk.

**Many-keys regime (the b2c hot corpus copied read-only into a private cluster: 2,600,000
keys with one support each, 9,002 active scopes, half precise and half comprehensive,
1,300,999 precise winners, CVE-HOT on 520,085 keys, `pkg:hot` on 1,600,000 keys).**
This is the corpus the hot-key ruling points at, and it is harsher than the arbiter's
supports definition: 1,600,000 supports on one package is 1,600,000 keys here. The V
statement costs about 2 us per winners row that matches the selectors with an exact
status (a sequential scan of winners, then one ordered-index probe per failing winner).

| Read | median | p95 (max of 8 to 20) | plan |
| --- | --- | --- | --- |
| list, no selector, status | 5,170.9 ms | 5,206.0 | 1,300,999 rows through a top-N sort, 1,299,001 probes |
| list, package `pkg:hot` | 3,637.0 ms | 3,690.9 | the same shape over 1,600,000 matching keys |
| list, CVE-HOT and `pkg:hot` | 1,614.3 ms | 1,629.4 | |
| list, CVE-HOT | 1,936.9 ms | 6,055.9 (first run) | |
| count, no selector | 5,217.0 ms | 5,521.4 | de-duplication sort spills 48,408 kB to disk (`temp read=6051 written=6056`) |
| count, package `pkg:hot` | 3,655.3 ms | 3,759.9 | |
| inventory by status, no selector | 4,979.2 ms | 5,104.7 | |
| inventory by status, package `pkg:hot` | 3,536.1 ms | 3,595.1 | |

That misses the 1,500 ms bar and shows a temp file sized by the match set, two of the
ruling's FAIL conditions, so this returns to the arbiter with the raw plans
(`hot_key_plan_*.txt`). It scales linearly with the matching keys: 80,000 keys (QA)
reads at 226 to 238 ms, 500,000 keys would read at about 1.0 s, 750,000 at about 1.5 s.
The legacy comparison on this corpus is not run: updating 2,600,000 finding facts to the
mutated values did not finish in 52 minutes and was cancelled, so the legacy read has
no consistent facts here.

**JIT 20 % rule, end to end through the Go stores, with and without the wrapper**
(same store, `BeginTx` hidden for the "without" side, interleaved):

| Read | jit on, no wrapper | wrapper (jit off) | net saving | executions |
| --- | --- | --- | --- | --- |
| list status | 242.1 ms | 243.8 ms | -0.7 % | steady state, generic plan (n = 25) |
| count status | 243.3 ms | 243.5 ms | -0.1 % | steady state |
| count, no selector | 242.7 ms | 243.7 ms | -0.4 % | steady state |
| list status | 1,663.5 ms | 287.6 ms | 82.7 % | fresh connection, custom plan (n = 12) |
| count status | 1,607.6 ms | 293.0 ms | 81.8 % | fresh connection |
| count, no selector | 1,610.3 ms | 290.0 ms | 82.0 % | fresh connection |

pgx's statement cache runs a statement's first five executions on each connection on a
custom plan, which PostgreSQL compiles with JIT (`Functions: 179`, inlining and
optimization on); later executions reuse a generic plan that is not compiled. The wrapper
removes a 1.3 s penalty from every custom-plan execution and is neutral otherwise (0.06 ms).

## Limits

- The shim runs on a shared host at load 6 to 17, and the theory-proof JIT pair
  was measured at a different load; these rows are not comparable to the theory
  proof's `jit = on` rows, only to the bar.
- Payloads are padded text on the winners row and the best support of each key.
  Production width is not known; 6.25 KB is a stated stand-in, and the page read
  is 201 rows.
- The 1,600,000-key regime misses the bar (see the hot-key section).
- The hot image (160,000 supports) was not re-run through the product statements.
  The theory proof sized it at about 50 to 85 ms per 10,000 supports; ruling
  decision 7 accepts it as a documented limit.
- The supports table here is current by construction (loaded directly, marker
  inserted), not written by the maintainer, and the winners watermark is set to
  now.

Observability Evidence: `eshu_dp_supply_chain_impact_read_route_total{route}` and the
`eshu.supply_chain.impact.read_route` span attribute name the read model of every
call; a refused call counts the route it tried; the 503 envelopes name the model
(supports or winners) that is not ready.

**NOT_CHECKED:** QA candidate counts and skew, the QA replica latency and cold read,
real maintainer-written supports, concurrent writers during a read, the hot image
through the product statements, production payload width, the supplement candidate
count on the span.
