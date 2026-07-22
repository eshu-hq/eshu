# Graph-read deadline evidence (#5273)

## Contract

`Neo4jReader` gives every logical read one 10-second client budget unless the
caller deadline is earlier. The remaining time is also passed to
`neo4j.WithTxTimeout`. A typed driver-classified retryable connectivity error
may open one replacement read session, for two attempts total under the
original budget. Other errors are not retried.

Session cleanup does not reuse the expired read context. The reader waits for
`Session.Close` with a separate one-second bound so the driver can return the
connection to its pool. Cleanup failure does not replace an otherwise valid
read result, but emits the sanitized
`query.graph_read.session_close_failed` warning without driver text, query
text, or backend addresses.

The caller context is classified before the policy child context. An enclosing
deadline or cancellation therefore returns unchanged and records
`caller_deadline` or `canceled`; it cannot be counted as a graph-policy
`deadline`. A policy-child expiry or backend transaction timeout returns
`ErrGraphReadDeadline`. API and MCP startup backfills use the same reader, so a
blocked owner-ledger page is bounded by this policy too.

The read-only Cypher route keeps its 30-second outer request budget for parsing,
authorization, response shaping, and transport work. Its graph execution still
uses the reader's tighter 10-second budget. Tests compose the real reader with
the handler and assert both deadlines, so a fake graph port cannot hide drift
between the two layers.

Deadline and availability failures retain their causes for `errors.Is` and
`errors.As`, but API and MCP responses use stable messages and
`backend_timeout` or `backend_unavailable` codes. Raw driver text, graph
addresses, and Cypher are excluded from the response and warning event.

## Accuracy and edge-case proof

Focused tests cover healthy reads, an already-canceled caller, an earlier
caller deadline, expiration during result collection, backend transaction
timeout codes, an unreachable backend at query start, a disconnect during
collection, a recovered second attempt, and a non-retryable query error.
Handler and MCP dispatch tests cover the stable 503 and 504 envelopes and
private-cause suppression.

## Performance and concurrency evidence

Performance Evidence: the policy does not change query shape, result
conversion, graph writes, or healthy backend round trips. A fixed-input Go
microbenchmark (`BenchmarkNeo4jReaderHealthyPolicyOverhead`, 10,000
iterations, five runs, darwin/arm64) uses the same fake driver/session factory,
query and parameters, no-op tracing, row conversion, and one
session/Run/Collect/Close lifecycle for both readers. The unbounded control
measured 793.1-903.9 ns/op (837.2 ns/op median), 1,248 B/op, and 19 allocs/op.
The bounded reader measured 1,272-1,420 ns/op (1,395 ns/op median), 1,544 B/op,
and 25 allocs/op: a 557.8 ns median fixed-cost increase, 296 B, and 6
allocations per healthy logical read. The widest cross-run difference was
626.9 ns. This measures only fixed in-process overhead, not a Bolt
round trip.

Concurrency Evidence: the policy adds no lock, queue, lease, worker, goroutine,
or shared mutable state. A retry uses a fresh session only for a typed
retryable connectivity failure. A
25-millisecond bounded delay prevents an immediate reconnect loop without
serializing callers. Amplification is capped at two attempts inside one budget.

## Observability evidence

Observability Evidence: every logical read records one Neo4j query-duration
datapoint with
`operation="read"` and a closed outcome. The `neo4j.query` span records the
same outcome, attempts, and configured deadline. Slow, deadline, and
unavailable reads emit one sanitized `query.graph_read.warning` event.

## Prior source-commit runtime proof

The source implementation at commit `3de4afa5c8` was exercised in an isolated
branch-built API and MCP run using NornicDB v1.1.11 at digest
`sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`.
Both services used that commit's production `Neo4jReader` wiring and canonical
truth envelope. These timings are retained as source-commit evidence, not
represented as a fresh live run of the current port. Timings include the HTTP
transport boundary.

| Case | API | MCP |
| --- | --- | --- |
| Healthy `RETURN 1` | HTTP 200, exact/fresh, 6.334 ms | JSON-RPC success, exact/fresh, 6.146 ms |
| Query exceeding the reader budget | HTTP 504, `backend_timeout`, 10.003164 s | JSON-RPC error result, `backend_timeout`, 10.003776 s |
| Graph unreachable at query start | HTTP 503, `backend_unavailable`, 29.546 ms | JSON-RPC error result, `backend_unavailable`, 30.671 ms |
| Graph stopped during a CPU-bound query | HTTP 503, `backend_unavailable`, 593.953 ms | covered by the same shared dispatch contract test |

The graph health endpoint remained HTTP 200 after the API deadline case. API
warnings reported `deadline` at 10.001506708 seconds, start-unavailable at
0.025617333 seconds, and mid-query unavailable at 0.591184333 seconds. MCP
warnings reported `deadline` at 10.001005750 seconds and unavailable at
0.026364834 seconds. The warnings contained only the bounded event, phase,
failure class, duration, and standard telemetry context.

The isolated API, MCP, NornicDB, and Postgres processes were stopped after the
proof, and both disposable containers were removed. The retained API,
Postgres, NornicDB, MCP, projector, and collector container identities matched
their pre-proof identities and all remained healthy.

## Current surface inventory and exact-head probe

The current target manifest is generated from authoritative code instead of a
fixed historical count. `go run ./cmd/capability-inventory -mode verify` reads
every HTTP method and path from `query.OpenAPISpec`, every MCP tool from
`mcp.ReadOnlyTools`, and verifies the committed
`go/internal/capabilitycatalog/data/surface-inventory.generated.json` artifact.
`TestSurfaceInventoryDriftAgainstRealCode` prevents an added or removed route or
tool from silently bypassing the inventory.

The generated inventory is exhaustive for current OpenAPI and MCP registry
names; the probe manifest adds five directly registered HTTP surfaces that are
not all represented in OpenAPI. The
checked-in `graph-read-probe` registry is deliberately narrower: it covers the
seven direct #5273 graph-read entry points across API and MCP, including
repository inventory, empty-selector repository statistics, direct Cypher,
and visualization. It supplies valid bounded arguments, labels the required
user-token versus admin/all-scope posture, validates its API/MCP names against
the current registries, and fails closed on authentication, unsupported
routes/tools, non-2xx responses, JSON-RPC errors, or MCP tool-error results.

Run it against branch-built exact-head API and MCP endpoints from the `go`
directory:

```bash
ESHU_API_BASE_URL=https://api.example.invalid \
ESHU_MCP_URL=https://mcp.example.invalid/mcp \
ESHU_MCP_TOKEN=... ESHU_API_KEY=... \
go run ./cmd/capability-inventory -mode graph-read-probe
```

`ESHU_MCP_TOKEN` is the normal user bearer credential used for repository
reads. Direct Cypher and visualization are intentionally shared-key/all-scope
surfaces, so the runner requires the separately labeled admin credential in
`ESHU_API_KEY`; it never prints either value. This commit adds the executable
runner and local transport/authentication proof only. It does not claim that a
current exact-head remote run has occurred.

The recovered historical exhaustive harness enumerated 248 HTTP routes and
159 MCP tools (407 targets) on an older commit. Its retained rows contain local
identifiers and stack metadata and are not copied into this repository. The
current code-derived manifest contains 415 unique targets (the current OpenAPI and MCP
registries plus five directly registered HTTP surfaces). Only seven currently
have safe checked-in fixtures, so the command intentionally exits non-zero
after those probes and reports that 408 current surfaces remain unsupported.
That explicit failure is the exact-head burn-down gate: unsupported surfaces
cannot be silently counted as validated, and no current exhaustive remote run
is claimed until the fixture count reaches the manifest count.
