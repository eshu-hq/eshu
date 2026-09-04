# Agent instructions: advisory

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's
  `supply_chain_advisory_alias.go` already imports this package for its
  compatibility aliases, so the reverse import cycles. If a change needs
  something only root exposes, either a leaf equivalent already exists
  (`querycontract`, `querydecode`) or it does not belong in this family;
  ask before adding one.
- The capabilities (`AdvisoryCatalogCapability`,
  `AdvisoryEvidenceCapability`) are registered in ROOT
  (`contract_supply_chain.go`), not here — root owns the router and always
  links into production. This package only declares the constant values.
- The typed decode wrappers MUST return `*querydecode.Error` via
  `querydecode.New`, never root's `newQueryDecodeError`. The model drops on
  any non-nil error without inspecting its type, and the dead-letter tests
  pin the drop, not the error type — but the next family copying this seam
  copies the constructor call too, so keep it on the leaf.
- `supplyChainDefaultSchemaMajorVersion` MUST stay `"1.0.0"` and MUST stay
  a family-local copy (packagereg precedent). It mirrors root's
  `queryDefaultSchemaMajorVersion`; if the schema major ever moves, both
  change together — grep for both names.
- `derefString`, `derefFloat64`, `mapVal`, `stringMapSliceVal` are
  family-local copies of trivial root helpers. They MUST stay
  behavior-identical to their root sources (named in each provenance
  comment). Do not extend them with family-specific semantics; add a new
  helper instead.
- `AdvisoryEvidenceFilter` MUST carry an anchor: `HasScope` gates the
  store, and the staying root handler gates before it. Widening either
  gate enables unscoped reads over the whole vulnerability corpus.
- The catalog SQL MUST keep its bounded single-pass shape (#3389): one
  `GROUP BY` over per-kind `UNION ALL` legs, `FILTER`ed aggregates, no
  `MATERIALIZED` CTEs, no rollup joins. The per-kind active-scan anchors
  keep the #3402 partial indexes eligible. Root catalog tests pin both;
  run them after any SQL touch.
- Files must stay under 500 lines. `supply_chain_advisory_evidence_model.go`
  (463) and `supply_chain_advisory_evidence_sql.go` (356) are the ones to
  watch; split by concern (decode, grouping, key normalization) rather
  than growing them.

## Exported symbols and why each is exported

Every export below names a staying root caller — no speculative API. Do
not export a new symbol without adding its caller to this list.

- `AdvisoryCatalogCapability`, `AdvisoryEvidenceCapability` —
  `contract_supply_chain.go` registration; the catalog, evidence, and
  vulnerability-detail handlers.
- `AdvisoryCatalogMaxLimit`, `AdvisoryEvidenceMaxLimit` — the staying
  root handler limit checks and the root catalog/evidence tests.
- `AdvisoryEvidenceMaxFactRows`, `AdvisoryEvidenceFactCapacity` — the
  root evidence tests (`FactCapacity` also bounds the store's scan).
- `NormalizeAdvisoryEvidenceFilter`, `NormalizeAdvisoryCatalogFilter`,
  `AdvisoryEvidenceFilter.HasScope` — the staying root handlers
  (evidence, vulnerability-detail) and the root tests.
- `BuildAdvisoryEvidenceRows`, `AdvisoryEvidenceFactRow`,
  `CanonicalAdvisoryKey`, `PageAdvisoryEvidenceRows`,
  `AdvisoryEvidenceLookupIDs`, `ListAdvisoryEvidenceQuery`,
  `ListAdvisoryCatalogQuery` — the root evidence/catalog/SQL tests, which
  pin grouping, paging, normalization, and SQL shape.
- `AdvisoryEvidenceQueryer` — the constructor parameter the root tests,
  the root alias forwarders, and `cmd/*` wiring name.
- `FormatNullTime` — the staying root work-item evidence store.
- `SetToSortedSlice` — the staying root work-item evidence state helper.
- Store/filter/row types, store structs, and constructors — the staying
  root handlers (`SupplyChainHandler` fields), vulnerability-detail
  handler, tests, and `cmd/*` wiring via the root aliases.

## Where the tests live

All six advisory test files stay in root package `query` for this lane —
do not "reunite" them here:

- `supply_chain_advisory_catalog_test.go`,
  `supply_chain_advisory_evidence_test.go`,
  `supply_chain_advisory_evidence_decode_test.go`,
  `supply_chain_advisory_evidence_scope_test.go`,
  `supply_chain_advisory_evidence_scoped_token_test.go`,
  `supply_chain_advisory_evidence_sql_test.go`, plus
  `supply_chain_vulnerability_detail_handler_test.go` and the
  vulnerability snapshot lockstep test.

The handler-driving tests reach the staying root handlers through `Mount`
and a real mux, so a route or request-shape change fails in root, not
here. The unit tests share root helpers (`factRow`,
`recordingAdvisoryEvidenceStore`, `unusedAdvisoryEvidenceQueryer`) with
those handler tests; splitting the helpers now would fork them. The hub
PR3 moves the handlers and re-homes the suite. Until then, reach moved
symbols from root tests as `advisory.X`.

## Shared test fixtures

`querytestutil` holds the fixtures both this package's future tests and
root need. Put a new shared fixture there rather than copying it. Reuse
its doubles; never redeclare them.

## Common changes

- New vulnerability fact kind on the evidence path: add the kind to
  `advisoryEvidenceFactKinds`, extend the SQL legs, add the accumulator
  branch in `supply_chain_advisory_evidence_model.go`, and extend the
  root evidence tests (grouping + SQL shape + lockstep). All four, or the
  kind is silently dropped or unpinned.
- New response field backed by a typed struct: check the
  struct-completeness note in `supply_chain_advisory_decode.go` first —
  if the sdk struct does not declare the field, the read stays raw with a
  struct-gap comment, same as the existing ones.
