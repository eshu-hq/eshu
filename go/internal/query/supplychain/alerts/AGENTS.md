# Supply-Chain Alerts — Agent Instructions

Scope: `go/internal/query/supplychain/alerts/` (package `alerts`).

## Ownership

This leaf owns the security-alert reconciliation Postgres reads (#6642):
`store.go` (`PostgresStore`, `Queryer`, the list read, the
provider-repository-scope lookup, the row decoder), `queries.go` (the list
SQL text), `aggregates.go` (`PostgresAggregateStore`, `AggregateQueryer`,
the count and inventory reads, the dimension-to-SQL-expression switch),
`aggregate_queries.go` (the aggregate SQL text), and `triage.go` (the
row-level triage-detail decoder), plus `queries_test.go`,
`aggregates_test.go`, `store_test.go`, and `triage_test.go` (this package's
own tests, several moved in verbatim from root -- see README.md's Move
evidence).

## Invariants

- MUST NOT import root package `query` or `supplychain` would import this
  package for the compatibility aliases in `supply_chain_hub_alias.go`,
  cycling back. Reach root-only helpers through `supplychain` (port
  interfaces, filter/row/limit types) or `querycontract` (payload
  row-value decoders); if neither has what you need, it does not belong
  here -- ask before adding a new shared home.
- `factKind` MUST stay `"reducer_security_alert_reconciliation"` --
  byte-identical -- it is a route-serves-data registry evidence marker
  (`docs/internal/design/5584-route-serves-data-registry.md`) and a
  reducer-derived-fact-governance citation
  (`docs/internal/design/4784-reducer-derived-fact-governance.md`).
- The SQL text (`listQuery`, `aggregateTotalQuery`,
  `aggregateGroupQueryTemplate`, `inventoryQueryTemplate`,
  `sourceFreshnessGroupExpr`, `providerRepositoryScopesQuery`) and
  `inventoryGroupExpression` stay unexported -- no speculative API. The
  tests that pin their exact text and grant-predicate ordering live
  in-package (`queries_test.go`, `aggregates_test.go`), moved verbatim from
  the root scope/aggregates tests. Do not re-export these to serve a root
  test; move the test here instead.
- `Queryer` and `AggregateQueryer` are exported because they are
  `NewPostgresStore`/`NewPostgresAggregateStore`'s parameter types: root's
  `NewPostgresSecurityAlertReconciliationStore`/
  `NewPostgresSecurityAlertReconciliationAggregateStore` forwarders in
  `supply_chain_hub_alias.go` must be able to name them, and through those,
  `cmd/api`/`cmd/mcp-server` wiring (which passes a `*sql.DB`, satisfying
  both) depends on them transitively.
- `StringMapVal` is exported because root's `sbom_attestation_attachments.go`
  and `sbom_attestation_attachment_rows.go` still call it through the
  `stringMapVal` forward in `supply_chain_hub_alias.go`. `mapVal` and
  `stringMapSliceVal` have no such caller; keep them unexported.
- `ListSecurityAlertReconciliations`, `SecurityAlertProviderRepositoryScopes`,
  `CountSecurityAlertReconciliations`, and
  `SecurityAlertReconciliationInventory` are the exact method names the
  `supplychain.SecurityAlertReconciliationStore` /
  `SecurityAlertReconciliationAggregateStore` port interfaces require --
  they are a port contract, not stutter; do not shorten them.
- The `var _ supplychain.SecurityAlertReconciliation(Aggregate)?Store = ...`
  compile-time pins in `store.go` and `aggregates.go` moved here from root's
  `supply_chain_hub_alias.go` (#6642) -- keep them beside the type they pin
  rather than moving them back to root.

## Exported symbols and why each is exported

- `PostgresStore`, `NewPostgresStore` -- root's type alias and constructor
  forwarder in `supply_chain_hub_alias.go`, which `cmd/api` and
  `cmd/mcp-server` wiring call.
- `PostgresAggregateStore`, `NewPostgresAggregateStore` -- same, for the
  aggregate store.
- `Queryer`, `AggregateQueryer` -- the constructor parameter types; callers
  are the root forwarders above plus, transitively, `cmd/api`/
  `cmd/mcp-server` wiring.
- `StringMapVal` -- the two staying root SBOM files, via the root
  `stringMapVal` forward.
- The four port methods listed above (port contract, not stutter).

No other symbol in this package is exported.

## Naming

`docs/internal/naming.md` is law: no `security_alert_` file prefixes, no
`alerts/security_alert.go`, exported identifiers lose the family stutter
except where a port interface fixes the method name (see above). The root
`supply_chain_hub_alias.go` keeps every old exported spelling, and the
unexported `stringMapVal` forward, for staying callers.
