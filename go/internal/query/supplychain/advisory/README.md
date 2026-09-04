# Advisory query read models

## Purpose

Reads the supply-chain vulnerability-intelligence surface from active
vulnerability source facts in Postgres: the browsable advisory catalog
(`GET /api/v0/supply-chain/advisories`, no anchor required) and the
source-only advisory evidence detail
(`GET /api/v0/supply-chain/advisories/evidence`, anchored to an advisory,
CVE, package, repository, service, or workload). Rows are source
intelligence only and never imply repository, image, workload, or
deployment impact.

## Ownership boundary

This package owns the two read models: the catalog and evidence store
ports, their Postgres implementations, the bounded SQL texts, the
fact-grouping read model (`BuildAdvisoryEvidenceRows`), the typed
factschema decode wrappers for the four vulnerability kinds, and the
capability and limit constants. It does not own auth, the HTTP handlers,
the response envelope, or capability registration — those stay in root
package `query` until the hub PR3 (see below).

Root package `query` keeps the handlers (`supply_chain_advisory_*_handler.go`,
`supply_chain_vulnerability_detail_handler.go`), the capability matrix
rows (`contract_supply_chain.go`), the `SupplyChainHandler` struct, and a
minimal compatibility alias file (`supply_chain_advisory_alias.go`) with
the two store types and constructors `cmd/api` and `cmd/mcp-server` still
call as `query.NewPostgresAdvisory*`. Root performs capability
registration deliberately: root owns the router and always links into the
production binary.

## Exported surface

The store ports `AdvisoryCatalogStore` and `AdvisoryEvidenceStore`, the
Postgres implementations and their constructors, the values crossing those
ports (`AdvisoryCatalogFilter`, `AdvisoryCatalogPage`, `AdvisoryCatalogRow`,
`AdvisoryEvidenceFilter`, `AdvisoryEvidenceRow` and its evidence structs),
the grouping entry point `BuildAdvisoryEvidenceRows` with its fact-row and
key helpers (`AdvisoryEvidenceFactRow`, `CanonicalAdvisoryKey`,
`PageAdvisoryEvidenceRows`, `AdvisoryEvidenceLookupIDs`,
`NormalizeAdvisoryEvidenceFilter`, `NormalizeAdvisoryCatalogFilter`,
`AdvisoryEvidenceFactCapacity`), the SQL texts (`ListAdvisoryCatalogQuery`,
`ListAdvisoryEvidenceQuery`), the capability and bound constants
(`AdvisoryCatalogCapability`, `AdvisoryEvidenceCapability`,
`AdvisoryCatalogMaxLimit`, `AdvisoryEvidenceMaxLimit`,
`AdvisoryEvidenceMaxFactRows`), and the shared seams other root read
models reuse (`AdvisoryEvidenceQueryer`, `FormatNullTime`,
`SetToSortedSlice`). Every export names a staying root caller; see
`AGENTS.md` for the per-symbol list. See `doc.go` for the godoc-rendered
contract.

## Dependencies

Internal packages, all of them leaves that never import root package `query`:

- `internal/query/querycontract` — `StringVal`, `BoolVal`,
  `StringSliceVal` row-value decoders (root forwards to the same
  functions, so behavior is identical on both sides of the move).
- `internal/query/querydecode` — the classified decode failure the four
  vulnerability decode wrappers return (packagereg precedent).
- `internal/storage/postgres/pgarray` — the array scan/build surface the
  stores read and write through.

Plus `sdk/go/factschema` (typed vulnerability decode seams) and the
standard library. The `mapVal`/`stringMapSliceVal` payload helpers and the
`derefString`/`derefFloat64` nil-safe derefs are family-local copies of
trivial root helpers that cannot cross the package boundary; each carries
a provenance comment naming its root source.

## Telemetry

This package emits no metrics, spans, or logs of its own. The advisory
routes are traced in the staying root handlers
(`telemetry.SpanQueryAdvisoryCatalog`, `telemetry.SpanQueryAdvisoryEvidence`).

## Move evidence (#6060)

This package was created by moving eight files out of root package `query`
(`git mv`, no logic changes). The two assertions below are structural
rather than promissory — each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a rewrite.
`git diff -M --find-renames` pairs each file with its root predecessor;
the only statement-level changes are the `package` clause, the
`advisory.` qualification at staying root call sites, the export renames
listed in `AGENTS.md`, the `querycontract` qualification of the value
decoders (root forwards to the identical functions), and the family-local
copies of the decode seam, deref helpers, and map helpers, each documented
with its root source and verified byte-identical in behavior by the
unchanged root test suite (`go test ./internal/query/...`), which pins the
grouping, paging, normalization, SQL shape, and dead-letter behavior.

No-Observability-Change: there is no observability to change — this
package emits none, and the handler spans and capability strings keep
their exact values (the capability constants moved byte-identical; only
their package qualifier changed).

## Gotchas / invariants

- Do not import root package `query`. Root's
  `supply_chain_advisory_alias.go` already imports this package, so the
  reverse import cycles.
- The capability is registered in ROOT (`contract_supply_chain.go`), not
  here — root owns the router and always links into production.
- `AdvisoryEvidenceFilter` must carry an anchor (`HasScope`); the store
  rejects anchorless reads before running SQL, and the handler rejects
  them before reaching the store.
- A dropped `Sources` entry is a dead-lettered malformed fact
  (`input_invalid`), not missing data. The typed decode wrappers drop
  rather than zero-fill; see the struct-completeness note in
  `supply_chain_advisory_decode.go` for which fields stay on the raw path
  and why.
- `AdvisoryEvidenceMaxFactRows` bounds the scanned fact rows behind one
  page; `AdvisoryEvidenceMaxLimit + 1` is the wire limit the pagination
  tests pin. Do not conflate the two.
- The catalog's bounded single-pass SQL shape (#3389) is pinned by root
  catalog tests: per-kind `UNION ALL` legs, one `GROUP BY`, no
  `MATERIALIZED` CTEs, no rollup joins. The per-kind active-scan anchors
  are pinned too (partial-index eligibility).
- The advisory tests stay in root package `query` for this lane (see
  `AGENTS.md`): handler-driving tests cannot leave root before the
  handlers do, and the unit tests share their helpers. Do not "reunite"
  them here until the hub PR3 moves the handlers.

## Related docs

- [HTTP API Reference](../../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../../docs/public/reference/telemetry/index.md)
