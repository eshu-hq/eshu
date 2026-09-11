# Supply-Chain Alerts

## Purpose

The Postgres reads behind the supply-chain hub's security-alert
reconciliation routes: the bounded list read, the provider-repository-scope
lookup, and the cheap-summary aggregate count and grouped inventory reads.

## Ownership boundary

Read-only Postgres store implementations for one supplychain family. Does not
own the HTTP handler (`supplychain.Handler`), the port
interfaces, or the filter/row/limit types the handlers and stores share --
those live in `supplychain/`. Does not own the reducer writer that produces
the `reducer_security_alert_reconciliation` fact
(`go/internal/reducer/securityalert/`) or the storage-layer active-fact view
(`go/internal/storage/postgres/facts_active_security_alert_reconciliation.go`)
-- those are separate families that happen to share the fact kind name.

## Layout

- `store.go` -- `PostgresStore`, `Queryer` (its constructor parameter port),
  the list read, the provider-repository-scope lookup, the row decoder, and
  the small payload-shaping helpers (`packageMissingEvidence`,
  `sourceFreshness`, `mapVal`, `StringMapVal`, `stringMapSliceVal`).
- `queries.go` -- `listQuery`, the list read's SQL text.
- `aggregates.go` -- `PostgresAggregateStore`, `AggregateQueryer` (its
  constructor parameter port), the count and inventory reads, and
  `inventoryGroupExpression`, the dimension-to-SQL-expression switch.
- `aggregate_queries.go` -- the aggregate SQL text (`aggregateTotalQuery`,
  `aggregateGroupQueryTemplate`, `inventoryQueryTemplate`,
  `sourceFreshnessGroupExpr`) built on the shared unexported
  `aggregateRankingCTE`.
- `triage.go` -- `missingEvidenceVal`, the row-level triage-detail decoder.
- `queries_test.go`, `aggregates_test.go`, `store_test.go`, `triage_test.go`
  -- the tests that pin this package's own unexported SQL text, decoder, and
  group-expression contracts in-package (see Move evidence).

## Move evidence

The family moved here verbatim from the query root
(`security_alert_reconciliation*.go`, #6642, split off the #6060 lane A
supplychain move); only the package clause, the `supplychain` /
`querycontract` qualifications, and the destuttered type, constructor, and
helper names differ. All SQL text and `inventoryGroupExpression` stay
unexported: this package's tests pin them in-package
(`TestSecurityAlertReconciliationSQLAppliesScopedGrant`,
`TestSecurityAlertReconciliationAggregateQueriesUseCurrentProviderAlertRows`,
`TestSecurityAlertReconciliationAggregateSourceFreshnessUsesCurrentFactAlias`,
`TestSecurityAlertReconciliationInventoryGroupExpressionEnumIsClosed` --
moved from the root scope/aggregates tests, bodies verbatim, names
requalified). `Queryer` and `AggregateQueryer` are exported because they are
the constructor parameter types root's forwarders in
`supply_chain_hub_alias.go` (and, through those, `cmd/api`/`cmd/mcp-server`
wiring) must be able to name; `StringMapVal` is exported because root's
`sbom_attestation_attachments.go` and `sbom_attestation_attachment_rows.go`
still call it, through the `stringMapVal` forward in
`supply_chain_hub_alias.go`.

No-Regression Evidence: baseline `origin/main` vs this branch --
`go test ./internal/query/...` and `go test ./internal/query/supplychain/alerts/`
pass with the same test-name union before and after the move (see the PR
description for the counts); the SQL query consts are byte-identical to the
pre-move text (normalized only for the identifier rename); the
route-serves-data registry entry for the reconciliation route points at the
new files with unchanged evidence markers, and its gate passes.

No-Observability-Change: this package emits no metric or span of its own.
The reconciliation routes keep `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total` via the unchanged query-surface
middleware, and the handler span name is unchanged.

## Related docs

- `docs/internal/design/5584-route-serves-data-registry.md`
- `docs/internal/design/4784-reducer-derived-fact-governance.md`
- `go/internal/query/read-models.md`
