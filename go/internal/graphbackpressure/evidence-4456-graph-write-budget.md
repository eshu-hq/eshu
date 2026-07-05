# Evidence: default graph-write in-flight budget (#4456 / #3624)

## Summary

Ships a bounded default for the graph-write in-flight budget
(`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=8`) in the shipped Compose and Helm
deployments. The backpressure gate machinery already exists (#4515 Lane B,
#4448 per-class split) but defaults to nil/passthrough when the env is unset, so
every shipped deployment ran with unbounded concurrent graph writes. Under a
concurrent bootstrap+reducer write storm that pushes NornicDB past its write
throughput knee, unbounded concurrency cascades into 30s canonical-write
timeouts, failed canonical projections, and dead-lettered work.

Classification: **reliability / correctness win** (prevents the concurrency-
saturation dead-letter cascade). Not a wall-clock win — backpressure paces
writes, raising transient queue depth during peak ingest by design.

## Theory proof — NornicDB concurrent-writer sweep

Throwaway shim (not committed): N threads, each issuing the timed-out write
shape back-to-back against a live NornicDB over the HTTP tx endpoint for an 8s
hold, measuring completed-writes/s, p50/p99, and 30s-timeout count per
concurrency level. Reproduce with the exact statement and harness below.

```
POST http://<nornic>:7474/db/nornic/tx/commit
statement: UNWIND $rows AS row MERGE (n:BenchFn {uid: row.uid})
           SET n.repo_id = row.repo_id, n.name = row.name, n.kind = row.kind
parameters.rows: 18 rows/write  { uid, repo_id, name, kind }
harness: for C in [1,2,4,8,12,16,24,32]: spawn C threads issuing the write in a
         loop for 8s; record per-request latency; report ok/s, p50, p99, and
         count of requests >= 29.9s (the 30s server timeout).
```

| concurrency | completed/s | p99 | 30s-timeouts |
|---|---|---|---|
| 8 | 3.2 | 7.2s | 0 |
| 12 | 5.0 | 9.5s | 0 |
| **16** | **5.4 (peak)** | 13.0s | 0 |
| 24 | 4.7 | 20.1s | 0 |
| 32 | 4.3 | **29.2s (at cliff)** | 0 |

Throughput **peaks near 12-16 concurrent writers then collapses** while p99
latency climbs linearly to the 30s canonical-write timeout. Bounding in-flight
writes to the knee gives PEAK throughput (5.4 vs 4.3 ok/s uncontrolled, +26%)
and keeps p99 well under the timeout — a paced budget that *raises* net
completed throughput, not a serialization workaround.

This gate is a **per-process** bound on each gated writer, not a global
cross-process ceiling: the reducer's aggregate gate (#4448) bounds one reducer
process's combined canonical+semantic in-flight to N, and bootstrap-index bounds
its own canonical writes to N, but nothing composes the two into a single global
budget. The default of 8 was chosen so the two writers that run concurrently in
the measured E2E (bootstrap-index + reducer) stay near the knee (their combined
in-flight ≈ 16 = the top of the zero-timeout plateau). A third concurrent gated
writer would add its own ≤N; a truly global budget across processes is a larger
design tracked separately.

## Work proof — full 909-repo clean-volume E2E (bootstrap + reducer)

Same corpus, backend, and machine; only `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`
changed. Before = unset (unbounded, current shipped default); after = 8.

| metric | unbounded (before) | budget=8 (after) |
|---|---|---|
| bootstrap exit | **rc=1 (failed)** | rc=0 (did not fail) |
| canonical projection failures | 100 | **13** |
| dead-lettered work items | 55 | **3** |
| 30s graph-write timeouts | 30 | 26 |
| — of which the Helm structural-edge shape | (mixed) | 22 |
| — of which concurrency/heavy-write | ~26 | **4** |

The budget cut the **concurrency-induced** timeouts from ~26 to ~4 and the
dead-letter cascade from 55 to 3, and kept bootstrap from the rc=1 catastrophic
failure. The residual 22 timeouts are a **separate, shape-induced** blocker
(the Helm `HelmTemplateValueUsage-[:REFERENCES]->` legacy-migration retract
scanning the 47k-edge shared REFERENCES type — a #4708-class planner miss,
tracked separately) that this budget does not and should not address.

## Observability

No new instruments; the existing `graphbackpressure` gate telemetry
(canonical/semantic/aggregate gate wait samples, #4448) now reports non-zero
wait under the default budget, which is the operator's signal to size the knob
to their backend. `ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK` (#3560)
remains the durable graph-write-timeout backpressure signal.

No-Observability-Change beyond activating the existing gate telemetry by default.

## Config surface

- `docker-compose.yaml`: `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT: ${...:-8}` on the
  gated writers bootstrap-index and resolution-engine only (the ingester's
  canonical writer is not yet gate-wrapped, so the knob is inert there and is
  deliberately not set — a NOTE marks it).
- `deploy/helm/eshu/values.yaml`: global `env.ESHU_GRAPH_WRITE_MAX_IN_FLIGHT: "8"`
  (read only by the gated writer processes; inert on api/mcp/collectors/ingester).
- `internal/envregistry/entries.go`: registers the knob + the two per-class
  overrides (#4448), regenerated reference doc.
- Operator escape hatch: set to `0` for legacy unbounded (passthrough).
