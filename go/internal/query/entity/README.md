# Entity handler family

## Purpose

`entity` holds the entity-handler family (Issue #6060, lane B B5): the
`EntityHandler` HTTP surface (`POST /api/v0/entities/resolve`, `GET
/api/v0/entities/{id}/context`, `GET /api/v0/workloads/{id}/context|story`,
`GET /api/v0/services/{name}/context|story`, `GET
/api/v0/investigations/services/{name}`), every method-home file behind
entity resolution (identity, page, results, workload, content-type, metadata,
summary, context-content shaping), the workload context core
(`fetchWorkloadContext`/`fetchServiceWorkloadContext` with the read-model
fallback) plus the runtime-topology, provisioned-platform, and platform
reads behind it, and the absorbed B4 `*EntityHandler` service seam (story
envelope, supply-chain enrichment, investigation, workload resolution). The
`platform_impact.context_overview` capability strings and the
`TruthBasisHybrid` envelope basis travel with the family; the shared
capability matrix row stays in the query root. The OpenAPI fragments
documenting the entity routes stay in the query root
(`openapi/paths/search/entities.go`), where scripts/verify-openapi.sh requires
every family's fragments to live.

The `EntityHandler` struct keeps its `Neo4j`, `Content`,
`CICDRunCorrelations`, `ContainerImageIdentities`, `SBOMAttachments`,
`Profile`, `Logger`, and `Instruments` dependencies, respelled onto the
leaf ports (`querycontract`, `supplychain`); `cmd/api` and `cmd/mcp-server`
wire it through the root `query.EntityHandler` alias unchanged.

## Ownership boundary

This package owns handler orchestration for the entity routes and the pure
shaping behind the entity reads. The `*ContentReader` target-support seam
stays in the query root: Go requires methods to live with their receiver
type, and `ContentReader` is a later lane's family. Shared read-model
loaders, row decoders, bounds, ports, envelopes, and the authorization seam
live in `querycontract`; selector resolution lives in `queryselector`;
service tech fingerprints and repo infrastructure reads live in
`repository`; service query-stage timing and evidence shaping live in
`service`; image/SBOM read models live in `supplychain`. This package
imports those leaves, never the reverse, and never the query root (the root
cycles back through `handler.go` and `entity_alias.go`).

Workload-context fetching (`FetchWorkloadContextForOperation`,
`FetchServiceReadModelWorkloadContext`) is this family's production seam to
the deployment-trace wrapper: the staying root
`family_impact_trace_deployment.go` builds an `EntityHandler` and calls it,
which is why those two methods are exported. The remaining exports
(`FetchWorkloadDeploymentTopology`, `FetchProvisionedPlatformResult`,
`ProvisionedPlatformResult`, `FetchWorkloadRuntimeTopology`) exist only for
staying root tests that pin family behavior; new callers must prefer the
HTTP surface or `querytestutil` doubles.

## Exported surface

Exports exist only for staying callers: the root deployment-trace wrapper,
the `cmd` wiring alias, and the staying root tests that pin family
behavior. Unexported helpers stay unexported; cross-package test pins go
through `querytestutil` (notably `FakePortContentStore` and
`FakeRepoGraphReader`) or `querycontract`.

## Dependencies

The package imports the Go standard library, `querycontract` (types, ports,
envelopes, shared bounds, authorization seam), `queryselector` (selector
resolution), `querytestutil`-adjacent fakes in tests only, `repository`
(tech fingerprint, repo dependency/infrastructure reads), `service` (query
timing, story/enrichment shaping), `supplychain` (image/SBOM read models),
and the `telemetry`/`log` packages for handler instruments. It never
imports the query root or graph drivers.

## Verification

Run focused `entity` tests, then root `query`, `queryplan`, `mcp`,
`cmd/api`, and `cmd/mcp-server` suites, plus whole-module build and vet.
Run `scripts/verify-package-docs.sh` whenever this package changes. The B-7
cassettes and B-12 snapshot must stay byte-identical: this family moves
code, never Cypher text or queue/projection behavior. The
`query-source-coverage.yaml` file keys move with the functions; digests
change only when a function source actually changes, proven by the queryplan
test.

No-Regression Evidence (#6060 lane-B B5): this package is a pure move of
the entity family from the query root (base a66064728) with no handler
logic changes — function bodies are identical modulo package qualifiers and
the documented export renames. Emitted Cypher text is byte-identical, pinned
by the queryplan production-binding tests (green) and the per-symbol
source_sha256 audits in `query-source-coverage.yaml`.

No-Observability-Change (#6060 lane-B B5): no new runtime behavior, so no
new spans, metrics, or logs. The existing entity telemetry events
(instruments, k8s-scan truncation disclosure) moved with their handlers
unchanged; operator signals are identical to base.
