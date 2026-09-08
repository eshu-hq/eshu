# Service evidence parsing leaf

## Purpose

`serviceevidence` holds the pure content extractors shared by the service
evidence stayer in package `query` and the repository narrative overviews in
package `repository` (Issue #6060, lane B): docs-route references, OpenAPI
spec summaries with `$ref` resolution, and the loose YAML/JSON document
accessors they are built from.

## Ownership boundary

This package owns parsing only. The portable evidence types
(`FileContent`, `ServiceAPISpecEvidence`, `ServiceAPIEndpointEvidence`) and
the `SpecFileResolver` port live in `querycontract`; this leaf imports
`querycontract`, never the reverse. Neither this leaf nor `querycontract`
imports the query root or any handler family. The upcoming service-family
move (lane B B4) consumes this same leaf; it must not grow
handler orchestration, graph queries, or SQL.

## Proof requirements

Moving code here must preserve extractor behavior exactly: run the root
`query` and `repository` package tests (service evidence, narrative
enrichment, docs routes) plus whole-module build and vet. The `#5720`
round-10 resolver error semantics (read failure is an error, absent file is
empty-with-nil-error) are covered by those suites; any change to ref
resolution must keep that distinction.
