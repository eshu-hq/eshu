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
call; a refused call counts the route it tried; the 503 envelopes name the model
(supports or winners) that is not ready.

**NOT_CHECKED:** QA candidate counts and skew, the QA replica latency and cold read,
real maintainer-written supports, concurrent writers during a read, the hot image
through the product statements, production payload width, the supplement candidate
count on the span.
