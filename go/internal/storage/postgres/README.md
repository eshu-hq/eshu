# internal/storage/postgres

## Purpose

`internal/storage/postgres` owns Eshu's relational persistence: facts, queue
state, content rows, status, recovery data, decisions, workflow control,
webhook and freshness triggers, Terraform-state read models, and reducer
support stores.

## Ownership boundary

This package owns Postgres schema and typed storage adapters. It does not own
collector observation, parser semantics, graph-write Cypher, reducer truth
decisions, or public HTTP/MCP handlers. Callers are responsible for preserving
the transaction, lease, freshness, retry, and idempotency contracts documented
on the store they use.

## Exported surface

See `doc.go` and `go doc ./internal/storage/postgres` for the contract. The
stable anchors are schema bootstrap, transaction wrappers, fact/content stores,
projector and reducer queues, workflow control, status/recovery stores, shared
projection intents, graph phase repair, webhook/freshness stores, and bounded
active-fact readers. Do not turn this README into an exported-symbol index.

## Dependencies

The package depends on fact models, workflow/scope models, reducer/projector
ports, query/status contracts, telemetry, and Postgres driver interfaces. Graph
execution stays in `internal/storage/cypher`.

## Telemetry

Postgres paths use `InstrumentedDB`, queue observer gauges, fact emission
spans, drift evidence spans, AWS checkpoint counters, shared acceptance upsert
metrics, queue claim/run metrics, and structured failure logs. Keep repository
paths, fact IDs, cloud IDs, and row payload details out of metric labels.

## Gotchas / invariants

- Schema bootstrap is idempotent and ordered by foreign-key dependency.
- Fact writes deduplicate by `fact_id` and sanitize JSONB before insert.
- Projector and reducer queue claim, heartbeat, ack, fail, replay,
  supersession, and reclaim paths must stay retry-safe and fenced.
- `ProjectorQueue.Ack` keeps supersession, activation, scope pointer update,
  and work success in one transaction.
- `/admin/status` must include pending shared projection intents and active
  shared projection leases so graph-visible reducer edges are not reported
  complete too early.
- Reducer read models use bounded active-fact indexes; API/MCP reads must not
  scan all fact rows.
- Terraform config-vs-state drift depends on byte-compatible dot-path
  flattening between parser config rows and state rows.

## Focused tests

```bash
cd go
go test ./internal/storage/postgres -count=1
go run ./cmd/eshu docs verify ../go/internal/storage/postgres --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/reducer/README.md`
- `go/internal/collector/terraformstate/README.md`
