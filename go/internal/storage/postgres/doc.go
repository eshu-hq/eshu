// Package postgres owns Eshu's relational persistence: facts, queue state,
// content store, status, recovery data, decisions, and workflow
// coordination tables.
//
// The package wraps the Postgres driver with OTEL-instrumented helpers and
// exposes typed access to queue claim, lease, batch, and recovery
// operations. Callers must respect transaction scope, lease timing,
// per-scope projector ordering, pending-or-active generation freshness checks,
// stale-generation coalescing, terminal-failure supersession, live-generation
// supersession, expired-lease priority, duplicate-lease reclaim, idempotency
// keys, and partial-failure behavior documented on each helper; queue and
// status writes are retry-safe by design and must stay that way. Supersession
// of projector rows and their scope generations must remain atomic. Schema and
// queue contract changes require migration and a matching update to the
// recovery and status surfaces. Status readers include pending shared
// projection intents in domain backlog aggregates because those rows gate
// whether reducer-owned graph edges are ready for query truth, and
// ReducerGraphDrain gives local NornicDB code-call projection a read-only view
// of reducer graph-domain backlog before it starts its edge write lane.
// FactStore kind-filtered reads use bounded, stable keyset pages, and payload
// value filters are available only for top-level payload fields that are part
// of a reducer domain's truth contract. Shared projection intent writes use
// bounded multi-row upserts so high-cardinality code-call materialization
// reduces Postgres round trips without changing idempotency semantics.
package postgres
