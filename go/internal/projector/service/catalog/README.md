# Service-catalog-correlation projector intents

## Purpose

This package recognizes service-catalog facts for one scope generation and
builds the reducer intent that asks the reducer to correlate those catalog
declarations — entities, ownership, repository links, dependencies, API and
operational links, scorecards, and warnings — against repository and
deployment truth.

## Ownership boundary

The package owns only the trigger selection and the reducer-intent value. The
root `internal/projector` package validates scope-generation boundaries,
rejects unsupported service-catalog schema versions before any builder runs,
constructs and owns the immutable fact lookup, preserves family order, and
owns projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainServiceCatalogCorrelation` handler owns the correlation decisions and
every write; the projector never mints service, ownership, or repository truth
from a catalog declaration.

## Exported surface

- `BuildServiceCatalogCorrelationReducerIntent` builds the
  `service_catalog_correlation` intent, anchored to the earliest fact in the
  generation whose kind the `facts.ServiceCatalogSchemaVersion` registry
  recognizes.

See `doc.go` for the full godoc contract.

## Dependencies

The builder depends on `internal/projector/intent.FactLookup` and
`internal/projector/intent.ReducerIntent`; this package must not import the
root projector package — root already imports this package to dispatch to it,
so the reverse import would cycle. It reads `internal/facts` for the catalog
schema registry, `internal/reducer` for the domain constant, and
`internal/scope` for the scope value whose `SourceSystem` is the last
source-label fallback. There is no decode seam: like `package/source`, this
builder reads only `envelope.FactKind` and never a payload field.

## Gotchas / invariants

- Every registry-recognized catalog kind is a trigger and none outranks
  another: the anchor is the earliest recognized fact in original input order
  across all nine kinds, via `FirstMatchingKindPredicate`. Replacing the
  predicate with an explicit kind list would silently drop a kind added to
  the registry later; adding a per-kind priority would change `FactID` for any
  generation that carries more than one kind.
- The `Reason` is always `service catalog facts observed` and the `EntityKey`
  is `service_catalog_correlation:<scope>`, one intent per scope generation
  regardless of how many catalog facts it carries. The root fan-out parity
  fixture pins both; the reducer reloads the generation's facts itself.
- `SourceSystem` falls back in three tiers: `SourceRef.SourceSystem`, then
  `CollectorKind`, then the ingestion scope's `SourceSystem`, each trimmed.
  `projectorintent.SourceSystem` stops after the second tier, so it is not a
  drop-in replacement here; the builder takes the scope value for the third
  tier and the focused test pins it.
- The payload is never decoded here, and the schema version is not checked
  here either. Root projection's `validateFactSchemaVersion` rejects an
  unsupported service-catalog `schema_version` before this builder runs, and
  its regression test stays at root because it asserts root behavior.

## Verification

Run the package contract tests, ordered fan-out parity and probe-count tests,
the projector package tree, package-doc and path mirrors, dirgate, telemetry
coverage, and the golden-corpus gates selected by the changed paths.

## Related docs

- [Projector architecture](../../README.md)
- [Intent contract](../../intent/README.md)
- [Service-catalog collector](../../../collector/servicecatalog/README.md)
- [Reducer domain catalog](../../../reducer/domain-catalog.md)
- [Package restructure](../../../../../docs/internal/design/package-restructure.md)
