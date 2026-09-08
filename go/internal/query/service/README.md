# Service handler family

## Purpose

`service` holds the service-handler family (Issue #6060, lane B): the
`ServiceCatalogHandler` HTTP surface (`GET
/api/v0/service-catalog/correlations` plus the local-descriptor evidence
reads behind it), every pure helper behind the service context, story,
investigation, evidence, hostname, ingress-posture, and deployment-evidence
reads, the service query enrichment with its row shaping, the service-story
dossier/overview/evidence-graph/supply-chain-scope/trace-path shaping, and
the family's `service_catalog.correlations.list` capability row. The OpenAPI
fragments documenting the service routes stay in the query root
(`openapi_paths_service_*.go`), where scripts/verify-openapi.sh requires
every family's fragments to live.

The `ServiceCatalogHandler` struct keeps its `Content`, `Correlations`,
and `Profile` dependencies; `cmd/api` and `cmd/mcp-server` wire it through
the root `query.ServiceCatalogHandler` alias unchanged.

## Ownership boundary

This package owns handler orchestration for the service routes and the pure
shaping behind the service reads. The `*EntityHandler` investigation/story/
workload-resolution methods and the `*ContentReader` target-support methods
stay in the query root: Go requires methods to live with their receiver
type. Those stayers call into this package's exported homes (`service_alias.go`
keeps every other caller compiling unchanged).

The deployment-trace enrichment the service enrichment consumes
(provisioning candidates, source chains, consumer enrichment, hostname
bounds) lives in `impacttrace`, not here: it is deployment family, shared
with the staying deployment-trace wrappers. Shared read-model loaders, row
decoders, bounds, and ports live in `querycontract`; YAML/OpenAPI content
parsing lives in `serviceevidence`; repository overviews live in
`repository`/`repositoryartifacts`. This package imports those leaves,
never the reverse, and never the query root (the root cycles back through
`handler.go` and `service_alias.go`).

The service capability (`service_catalog.correlations.list`) is declared by
this family in `capabilities.go` and registered through `querycontract`
like every other moved family; the root matrix no longer repeats it.

## Exported surface

Exports exist only for staying callers: the root stayers that consume
service reads (entity handlers, the service seam, the container-image
explanation, compare, the deployment-trace wrappers), the `cmd` wiring
aliases, the `serviceintelhttp` composer, and the staying root tests that
pin family behavior. Unexported helpers stay unexported; cross-package test
pins go through `querytestutil` or `querycontract`.

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds), `queryselector` (selector resolution),
`querytestutil`-adjacent fakes in tests only, `impact`/`impacttrace`
(deployment seams), `repository`/`repositoryartifacts` (deployment and
relationship overviews), `serviceevidence` (spec parsing), `supplychain`
(image/SBOM read models), `doctruth` (image-ref normalization), and the
`telemetry`/`log` packages for the `service_query.stage_*` events. It never
imports the query root or graph drivers.

## Verification

Run focused `service` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet.
Run `scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves
code, never Cypher text or queue/projection behavior. The
`query-source-coverage.yaml` file keys move with the functions; digests
change only when a function source actually changes, proven by the queryplan
test.

No-Regression Evidence (#6060 lane-B B4): this package is a pure move of
the service family from the query root (base 73ed3ad73) with no handler
logic changes — function bodies are identical modulo package qualifiers and
the documented export renames. Emitted Cypher text is byte-identical, pinned
by the queryplan production-binding tests (green) and the per-symbol
source_sha256 audits in `query-source-coverage.yaml` (2 legacy non_hot rows
converted to typed audits, digests verified).

No-Observability-Change (#6060 lane-B B4): no new runtime behavior, so no
new spans, metrics, or logs. The existing `service_query.stage_*`
telemetry events moved with their handlers unchanged; operator signals are
identical to base.
