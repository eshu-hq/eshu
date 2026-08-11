# Live Evidence Bundle Route Performance (#4045)

## Composition

`repositoryCountForEvidenceBundle` runs the identical
`MATCH (r:Repository) RETURN count(r) as count` `getIndexStatus` already runs
(status.go), through the same `GraphQuery` instance (`neo4jReader`), and
`liveEvidenceSnapshotFromReport` maps a `status.Report` loaded through
`loadStatusReport` -- the same `status.LoadReport`/full-selection call
`getPipelineStatus` and `listCollectors` already make -- into
`evidencebundle.LiveSnapshot`. The route adds no new query shape; it composes
three existing bounded, single-row-or-small-fixed-cardinality reads (one graph
count, one Postgres status snapshot, one in-process schema/redaction pass) into
one HTTP round trip. Measured live against a running stack, not estimated:

```bash
docker compose -f docker-compose.demo.yaml -p eshu-perfevid-4045 up -d --build
curl -sS http://127.0.0.1:18280/api/v0/status/index
for i in $(seq 1 20); do curl -sS -o /dev/null -w '%{time_total}\n' \
  http://127.0.0.1:18280/api/v0/evidence/bundle; done
```

Backend: Postgres 18.4 plus NornicDB (image `eshu-nornicdb-pr290:3722b483c02c`,
Bolt/HTTP protocol version 5.0.0), `ESHU_QUERY_PROFILE=local_full_stack`, on a
10-logical-CPU/64 GiB Apple Silicon host under Docker Desktop -- a relative,
same-machine comparison, not a claim against any named reference profile.
Corpus: the credential-free demo stack's fixed "acme" fixture, 6 repositories
and 21 ingestion scopes, reducer/projector queue fully drained and terminal
(`queue.total=151`, `outstanding=0`, `failed=0`, `dead_letter=0` from
`GET /api/v0/status/index`), so every measured read observes real, settled
status/graph state rather than a cold or still-draining stack.

## Measurements

20 warm requests per route (5 warm-up requests discarded first), two
independent samples:

| Route | Run | median | p95 | mean |
| --- | --- | ---: | ---: | ---: |
| `GET /api/v0/status/index` | 1 | 4.006 ms | 9.601 ms | 5.471 ms |
| `GET /api/v0/status/index` | 2 | 4.212 ms | 11.838 ms | 6.396 ms |
| `GET /api/v0/status/pipeline` | 1 | 4.068 ms | 7.413 ms | 4.525 ms |
| `GET /api/v0/status/collectors` | 1 | 3.661 ms | 5.870 ms | 4.076 ms |
| `GET /api/v0/evidence/bundle` | 1 | 5.654 ms | 6.207 ms | 5.796 ms |
| `GET /api/v0/evidence/bundle` | 2 | 5.864 ms | 12.282 ms | 6.994 ms |

The honest baseline is `GET /api/v0/status/index`, which runs the same
repository-count Cypher and a Postgres status read. The new route's median is
about 1.6-1.8 ms above that baseline across both samples -- consistent with
doing strictly more work per call (`getIndexStatus` loads the lighter
index-only selection; this route loads the full selection `status/pipeline`
and `status/collectors` also load, so its per-call cost sits between one
status route alone and the sum of the two heavier ones) -- and stays two to
three orders of magnitude below any operator-relevant latency budget for a
support-bundle export. It is materially cheaper than the sum of the three
source routes it replaces (`~4.0 + ~4.1 + ~3.7 = ~11.7 ms` sequentially,
matching what the CLI's `--live` path already pays across three HTTP round
trips) because it collapses them into one status snapshot load, one graph
read, and one in-process compose/validate pass. No regression: this route
does not replace, wrap, or sit in front of any of its three source routes'
existing call paths, so their own latency is unaffected. p95 jitter (up to
~12 ms) is symmetric across old and new routes and consistent with Docker
Desktop VM scheduling noise on an idle host, not a route-specific cost.

Raw samples backing the table above are recorded in
`docs/internal/measurements.jsonl` (`ledger:4045-status-index-median-run1`,
`ledger:4045-status-index-median-run2`, `ledger:4045-status-pipeline-median-run1`,
`ledger:4045-status-collectors-median-run1`,
`ledger:4045-evidence-bundle-median-run1`, `ledger:4045-evidence-bundle-median-run2`).

## Observability

No-Observability-Change: the handler emits no new span, metric, log line, or
runtime knob. It reuses the same instrumented dependencies `GET
/api/v0/status/index`, `GET /api/v0/status/pipeline`, and `GET
/api/v0/status/collectors` already read through -- the shared
`statusReader` (`pgstatus.NewInstrumentedStatusStore`, wired identically on
both `EvidenceHandler` and `StatusHandler` in `cmd/api/wiring_router.go` and
`cmd/mcp-server/wiring_router.go`) and the shared `neo4jReader`, whose
`RunSingle` already wraps every call in the `neo4j.query.single` span
(`internal/query/neo4j.go`). An operator diagnosing a failed
`GET /api/v0/evidence/bundle` at 3 AM reads the same Postgres status-store
metrics, the same `neo4j.query.single` span, and the response body itself:
`WriteError` returns a `503` naming "status reader not configured" or a `500`
naming the load/compose failure verbatim, so the failure mode is
self-describing without a dedicated span.
