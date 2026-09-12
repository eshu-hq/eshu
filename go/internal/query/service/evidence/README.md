# Service Evidence Parsing

## Purpose

`service/evidence` holds the pure content extractors shared by the service
evidence stayer in package `query`, the service evidence family in package
`service`, and the repository narrative overviews in package `repository`
(Issue #6060, lane B): docs-route references, OpenAPI spec summaries with
`$ref` resolution, and the loose YAML/JSON document accessors they are built
from.

## Layout

- `doc.go` — the godoc contract.
- `parse.go` — every extractor and accessor: `SliceValue`/`MapValue`/
  `StringValue`, `OpenAPIRefFilePath`, `ExtractDocsRoutes`/
  `LooksLikeDocsRoute`, and `ExtractAPISpecEvidence`/
  `ExtractAPISpecEvidenceWithoutRefs` with their `$ref`-resolution helpers.

## Ownership boundary

This package owns parsing only. The portable evidence types
(`FileContent`, `ServiceAPISpecEvidence`, `ServiceAPIEndpointEvidence`) and
the `SpecFileResolver` port live in `querycontract`; this leaf imports
`querycontract`, never the reverse. Neither this leaf nor `querycontract`
imports the query root or any handler family, including its own parent
package `service`: package `service` imports this leaf, so a back-import
cycles. It must not grow handler orchestration, graph queries, or SQL.

## Move evidence (#6642 Part D)

This package nested here verbatim from a glued top-level compound-name
package, splitting it into a directory per `docs/internal/naming.md` rules
2, 3, and 4: only the package clause, the file name
(`service_evidence_parse.go` → `parse.go`, dropping the package-name
stutter), the destuttered exports (`ServiceSliceValue` → `SliceValue`,
`ServiceMapValue` → `MapValue`, `ServiceStringValue` → `StringValue`), and
the four importers' import paths and qualifiers differ. No extractor
behavior, Cypher text, or queue/projection behavior changed.

## No-Regression Evidence

Baseline `origin/main` at the move vs this branch: `go test
./internal/query/...` passes with 0 failures; the extractor behavior is
pinned by the root `query` and `repository` service-evidence, docs-routes,
and narrative-enrichment suites, all exercising this leaf through its four
importers (`service/query_evidence.go`, `service/docs_routes.go`,
`service/query_evidence_types.go`,
`repository/narrative_enrichment.go`) unchanged. The `#5720`
round-10 resolver error semantics (read failure is an error, absent file is
empty-with-nil-error) are covered by those suites and preserved verbatim.

## No-Observability-Change

This package emits no metric, span, or log of its own; it is a pure
in-process parsing leaf. No new runtime behavior, so no new telemetry.
