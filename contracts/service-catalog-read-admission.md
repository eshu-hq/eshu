# ServiceCatalogCorrelationReadAdmission v0 (W8b pin record)

Refs eshu-hq/eshu#9669. Wave-5 ServiceCatalog tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the catalog legs.

## Shape

Version: `ServiceCatalogCorrelationReadAdmissionContractVersion = "v0"`
(go/internal/query/service/catalog_correlation_read_admission.go).

Exactly 3 reads, re-declared in this catalog-scoped shape instead of
embedding the shared ports — no drift, IaC, TerraformDrift, CodeGraph,
CodeRelationships, SemanticSearch, or SupplyChain surface rides along (the
codeowners pin re-declares the same correlation signature for its own
route; neither pin cites the other):

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListServiceCatalogCorrelations` | `CatalogCorrelationStore` | `GET /api/v0/service-catalog/correlations` (correlation read) |
| 2 | `ListServiceCatalogLocalDescriptorEvidence` | `CatalogLocalDescriptorEvidenceStore` | `GET /api/v0/service-catalog/correlations` (optional local-descriptor evidence read) |
| 3 | `MatchRepositories` | `querycontract.ContentStore` (graph argument nil) | `GET /api/v0/service-catalog/correlations` (non-canonical repository_id leg only) |

The third leg runs only for a non-canonical `repository_id`, through the
selector with the graph argument literally nil, so only
`ContentStore.MatchRepositories` runs on this route.

Route oracle codes (oracle `8a78eb3f48`): `GET
/api/v0/service-catalog/correlations` (`paths/service/catalog.go`:
200/400/403/500/501/503).

Transport: no constructor, selector, or `ReadStore` surface on this pin —
the compile-time mirror assertion proves the catalog-scoped re-declaration
matches all three staying handler-facing sources, and the satisfier
assertions prove the production Postgres store serves the first two pinned
reads with the minimal shape. (The third leg rides `*ContentReader` in
production, proven against `querycontract.ContentStore` in package query;
this package must not import the query root, so the pin mirrors that leg
through the `ContentStore` port.) Concrete-double-only proof is rejected.

Enforcement (all in go/internal/query/service):

- `catalog_correlation_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestServiceCatalogCorrelationReadAdmissionContractIsVersioned`, via
  `serviceCatalogCorrelationReadAdmissionMethodSet`),
  seal
  (`TestServiceCatalogCorrelationReadAdmissionWireShapesAreSealed` —
  exactly 3 reads), serve-the-route
  (`TestServiceCatalogCorrelationReadAdmissionWireShapesServeTheRoute`),
  and the seeded-drift RED half
  (`TestServiceCatalogCorrelationReadAdmissionSeededDriftIsDetected` — a
  planted widening must not match the frozen set).

## Satellite consumption

Depend on the shape, not a constructor: serve the catalog leg through the
pin — correlation plus optional local-descriptor evidence from the Postgres
store, selector reach through the content reader with the graph argument
nil — the same trio the leg runs against today. A satellite PR consuming
this pin for any other family is invalid without arbiter review.

## W8a disclosed-absence posture

`GET /api/v0/service-catalog/correlations` is natively mounted in-tree and
its OpenAPI fragment stays core-side (oracle `paths/service/catalog.go`,
codes above) — disclosed here, not silently dropped. No
`contracts/upstream/*` stub covers this leg: the wire-shape generator
snapshots forwarded relay spellings only, and no ServiceCatalog leg has one.

## Explicitly OUT

- `GraphQuery.Run`/`RunSingle` are OUT by construction: no `GraphQuery`
  method is exercised on this route.
- Selector 404/400/500 mapping and missing-evidence shaping stay handler
  behavior, not port shape: there is no method set to freeze.
- The grant checks, the fixed failure messages, and `QueryProfile` are
  handler behavior, not port shape.
- No constructor, selector, or `ReadStore` surface: the satellite satisfies
  this port with its own wiring; this pin carries the read shape only.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller depends
on it yet — so rollback moves no traffic and changes no data.
