// Package postgres owns Eshu's relational persistence: facts, queue state,
// content store, status, recovery data, decisions, and workflow
// coordination tables.
//
// The package wraps the Postgres driver with OTEL-instrumented helpers and
// exposes typed access to queue claim, lease, batch, and recovery
// operations. Callers must respect transaction scope, lease timing,
// per-scope projector ordering, stale-generation coalescing, terminal-failure
// supersession, live-generation supersession, expired-lease priority,
// duplicate-lease reclaim, idempotency keys, and partial-failure behavior
// documented on each helper; queue and status writes are retry-safe by design
// and must stay that way. Supersession of projector rows and their scope
// generations must remain atomic. Schema and queue contract changes require
// migration and a matching update to the recovery and status surfaces.
package postgres
