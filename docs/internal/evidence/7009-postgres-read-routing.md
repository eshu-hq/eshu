# PostgreSQL reader routing: local qualification

Date: 2026-09-30. Related work: #7009. This record qualifies the local
reader-routing implementation; it does not close deployed latency acceptance.

## Contract

API and MCP business queries use a PostgreSQL read-only port. Authentication,
revocation, control decisions, audit appends, startup and mutations use the
writer. A request captures a writer WAL checkpoint after authentication, releases
the writer connection, then checks the identity and replay position of the
physical reader connection before querying it. Failure returns an error; there
is no fallback to the writer.

The optional reader DSN defaults to the exact writer DSN for a single-instance
installation. Multiple hosts are connection candidates within bounded pools.
Writer candidates must identify the same primary incarnation. This is not
independent multi-primary support. Aurora endpoint configuration is a future
deployment option; Aurora identity, promotion and failover are unqualified.

Status snapshots use one bounded, read-only repeatable-read transaction.
Its connection is released before separate live-activity queries and response
serialization. Authentication is checked again on every Ask inner dispatch;
browser cookies and CSRF credentials remain browser credentials rather than
being replaced by the server's shared key.

## Correctness and failure proof

The physical-reader tests used disposable PostgreSQL 18.3 primary and streaming
standby instances. They exercised same-DSN routing, enforced read-only behavior,
replica replay waits, identity rejection, pool acquisition deadlines, connection
cleanup and row lifetimes. Hermetic normal/race tests cover snapshot begin,
read, commit, rollback, cancellation, panic and concurrent transaction failures.

A separate disposable database applied all 171 migration receipts on the rebased
source. API readiness returned 200 with the writer and standby. Actual API
wiring passed normal and race qualification for:

- Valid scoped tokens and browser sessions, including JSON and streaming Ask.
- Invalid, missing, revoked and expired tokens/sessions, and invalid CSRF.
- Browser revocation between successive Ask inner calls: the second call was
  denied before acquiring a checkpoint or querying business data.
- Successful business calls using one writer checkpoint and one reader query;
  denied calls using neither.

The browser escalation reproduction failed before credential forwarding was
corrected. A committed entrypoint regression invokes `engineAsker.Ask` and
`AskStream` with a real engine, MCP runner and authentication middleware. A
test-only overlay restoring the old header-only forwarding makes both browser
cases fail. Current source passes normal and race tests, preserves browser or
scoped-token grants, and keeps credentials out of provider messages.

Focused commands run successfully include:

```bash
cd go
go test -p 2 ./cmd/api ./cmd/mcp-server ./internal/query/admin/... \
  ./internal/runtime/... ./internal/ask/engine ./internal/askwiring -count=1
go test -race -p 2 ./internal/runtime/postgres \
  -run '^TestSnapshotStatusReader' -count=1
go test -p 2 ./internal/askwiring \
  -run '^TestEngineAskerReauthenticates' -count=1
go test -race -p 2 ./internal/askwiring \
  -run '^TestEngineAskerReauthenticates' -count=1
```

The actual API auth qualification used a test-only overlay and disposable
database credentials, not a production environment. Test overlays and detailed
logs are retained in the private review packet. These commands do not claim
that the live qualification tests are part of the default unit suite.

## Matched status cost

The final comparison used a warm, approximately 89 MB synthetic fixture, 20
alternating pairs per route, and the same pool, authentication, checkpoint and
fixture state within each pair. Both variants queried the guarded standby.
The baseline checked freshness for each status query; the candidate used the
committed snapshot reader. This isolates the transaction boundary, rather than
comparing a replica with legacy raw-primary queries.

The timed boundary was in-process `ServeHTTP` entry to return, **excluding
checkpoint acquisition and reader pre-fencing**. Every compared response was
200 and byte-equal after removing only the volatile top-level `as_of` field.

| Route | Per-query guard p95 | Snapshot p95 |
| --- | ---: | ---: |
| API pipeline | 34.332 ms | 19.865 ms |
| API operations, limit 2 | 28.922 ms | 13.488 ms |
| MCP direct pipeline | 39.245 ms | 20.228 ms |

The snapshot used one identity/replay fence per pipeline request. API operations
used two: the snapshot and a separate live-activity query. Connection-use
assertions were zero at snapshot return and HTTP completion. Reader-access and
snapshot spans identify the measured stages; fewer identity/replay checks are
the observed mechanism. Exact production wiring also passed natural request
smoke checks, separately from the pre-checkpointed comparison.

MCP operations was excluded because its live-activity board is intentionally
API/console-only. Both variants returned the existing 503; the exemption is
documented in `wiring_router_completeness_test.go`.

### Setup mutation disclosure

The harness was instructed to avoid fixture writes, but startup and denied-auth
smoke checks can append audit events. Inspection of the original fixture during
the run found six denied-read and three bootstrap events in the inspected time
interval. There was no pre-run audit count, so their attribution to setup is an
inference. This is a scope violation, not proof of a wholly read-only run.

Source places those setup calls before the paired measurements. WAL was not
continuously sampled, so zero incidental concurrent WAL writes is unproven.
The comparison describes the post-setup fixture state. Review retained the
matched evidence with this disclosure; no further live runs or cleanup were
performed. Audit rows, databases and volumes were preserved.

The immutable private final performance receipt has SHA-256
`b5cf9e195dbcccb6ef7a8f06cb0ea4315b7fd9e65a8aa2ddd38473f9c35610ad`.
It binds command exits, production/overlay/log hashes, stage counts and the
measurement boundary to source commit
`13cd97969d1c3064c2c7d4b281002ca6dd9bd9a3`. A subsequent telemetry correction
sets explicit seconds histogram boundaries. Its SDK export and repository-guard
regressions failed before the correction and pass afterward. Metric aggregation
configuration changed; business routing, SQL, checkpoint and snapshot logic,
and the original timing boundaries did not.

## Limits and deployment acceptance

This is local correctness and directional component-cost evidence. It does not
prove deployed p95, collector contention across 900+ repositories, cold-disk
latency, 100-engineer concurrency, Aurora failover or reduced reader memory.
The existing partial-schema performance fixture returned readiness 503; complete
migrations and readiness were proven on the separate qualification database.

The reader should initially match the writer's resources. Before downsizing or
claiming production capacity, measure representative concurrent API/MCP calls
while collectors write, including checkpoint cost, replay lag, pool wait,
end-to-end latency, errors and resource use. The separate infrastructure PR
provisions the initial reader; it has not been deployed by this work.
