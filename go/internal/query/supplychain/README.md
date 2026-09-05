# Supply-chain query hub

## Purpose

Owns the `SupplyChainHandler` HTTP surface: nineteen routes over
reducer-owned supply-chain truth, served from Postgres read models and the
graph (eleven in `Mount`, plus a count/inventory pair per aggregate
family).

| Route | Handler |
|---|---|
| `GET /api/v0/supply-chain/vulnerability-scanner/contract` | `getVulnerabilityScannerReadContract` |
| `GET /api/v0/supply-chain/sbom-attestations/attachments` | `listSBOMAttachments` |
| `GET /api/v0/supply-chain/advisories` | `listAdvisoryCatalog` (reads `advisory.`) |
| `GET /api/v0/supply-chain/advisories/evidence` | `listAdvisoryEvidence` (reads `advisory.`) |
| `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` | `getVulnerabilityDetail` |
| `GET /api/v0/supply-chain/impact/findings` | `listImpactFindings` (reads `impact.`) |
| `GET /api/v0/supply-chain/impact/explain` | `explainImpact` (reads `impact.`) |
| `POST /api/v0/supply-chain/impact/suppressions` | `createVulnerabilitySuppression` |
| `GET /api/v0/investigations/supply-chain/impact/packet` | `getImpactPacket` (composes via the injected packet responder) |
| `GET /api/v0/supply-chain/container-images/identities` | `listContainerImageIdentities` |
| `GET /api/v0/supply-chain/security-alerts/reconciliations` | `listSecurityAlertReconciliations` |
| impact aggregate routes | `supplyChainImpactAggregateRoutes` (count + inventory) |
| security-alert aggregate routes | `securityAlertReconciliationAggregateRoutes` |
| container-image aggregate routes | `containerImageIdentityAggregateRoutes` |
| sbom-attachment aggregate routes | `sbomAttestationAttachmentAggregateRoutes` |

Three runtime-evidence probes enrich impact findings before they are
served: the cloud probe (`supply_chain_impact_cloud_runtime_probe.go`),
the Kubernetes probe (`supply_chain_impact_kubernetes_runtime_probe.go`
plus the fairness fan-out in
`supply_chain_impact_kubernetes_runtime_probe_fair.go`), and the runtime
context applier (`supply_chain_impact_runtime_context_probe.go`). All
three promote a finding to `runtime_confirmed` only on current,
caller-authorized evidence; a nil inventory store disables its tier.

## Ownership boundary

This package owns the handler, the store ports, and the row/filter/page
values crossing them for the container-image, SBOM attestation, security
alert, and cloud-runtime families, plus the suppression mutation port,
the scanner contract shapes, the repository selectors, and the probe
budgets. The advisory and impact read models stay in the `advisory/` and
`impact/` subpackages; the Postgres store *implementations* stay in root
package `query` (shared with entity and incident-context reads) and
satisfy the hub ports through root's compatibility aliases.

Root package `query` keeps the capability matrix
(`contract_supply_chain.go`), the OpenAPI path fragments
(`openapi_paths_supply_chain*.go`), the cross-cutting auth and
graph-error sweeps, the `factschema_decode_supplychain.go` decoder, and
the lane-B packet envelope (`investigation_packet_*.go`). Root owns the
router and always links into the production binary, so capability
registration and the `SupplyChainHandler` compatibility alias
(`supply_chain_hub_alias.go`) live there. `cmd/api` and `cmd/mcp-server`
construct the handler as `query.SupplyChainHandler` exactly as before.

## Exported surface

Every export names a staying root caller; see `AGENTS.md` for the
per-symbol list. In brief: the handler and its result/request/response
types; the store ports and the filter/row/page/count values crossing
them; the capability, limit, and probe-budget constants the staying
contract matrix, staying stores, and staying tests read; the shared
seams staying root files reuse (`UniqueSortedNonEmpty`,
`SecurityAlertRepositoryScopeIDs`,
`SupplyChainCloudRuntimeProbePerDigestLimit`); and the
`SupplyChainImpactPacketResponder` port root implements from the lane-B
packet envelope. See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package
`query`:

- `internal/query/querycontract` — envelopes, capabilities, profiles,
  row-value decoders, repository access filter, collector readiness.
- `internal/query/queryauth` — auth context bounds and normalization.
- `internal/query/queryselector` — repository-selector resolution.
- `internal/query/queryspan` — handler span plumbing.
- `internal/query/supplychain/advisory`, `.../impact` — the advisory and
  impact read models.

Plus `internal/scope` (collector kinds), `internal/telemetry` (span
names), `internal/environment`, `internal/facts`, and
`sdk/go/factschema` (typed suppression decode). The `attachCollectorListReadiness`
copy, the tracer, and the graph-configured predicate are family-local
copies of trivial root helpers that cannot cross the package boundary;
each carries a provenance comment naming its root source.

## Telemetry

Handler routes open spans named in `internal/telemetry`
(`SpanQueryContainerImageIdentities`, `SpanQueryVulnerabilitySuppressionMutation`,
and the impact/advisory span names the moved handlers already used).
The cloud and Kubernetes probes open child spans carrying digest counts,
candidate counts, and authorization outcomes, so an operator can read
exactly why a finding did or did not promote to `runtime_confirmed`.
Span names, capability strings, and attribute keys are unchanged by the
move.

## Move evidence (#6060)

This package was created by moving twenty-five files out of root package
`query` (`git mv`, no logic changes). The two assertions below are
structural rather than promissory — each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a rewrite.
`git diff -M --find-renames` pairs each file with its root predecessor;
the only statement-level changes are the `package` clause, the
`querycontract`/`queryauth`/`queryselector` qualification of helpers
root forwards to the identical functions, the export renames listed in
`AGENTS.md`, the family-local copies documented above, and the packet
responder seam (the route's request parsing, store reads, and response
bytes are unchanged; composition still runs the same lane-B builder).
The moved test suite (`go test ./internal/query/supplychain/`) pins
handler, scope, probe, and readiness behavior from inside the package,
and the staying root suite (`go test ./internal/query/...`) pins the
routes, contract matrix, and packet parity through the aliases.

No-Observability-Change: span names, capability strings, attribute keys,
and the tracer seed (`queryspan.HandlerTracer`) are unchanged; only the
package qualifier moved.

## Gotchas / invariants

- Do not import root package `query`. Root's
  `supply_chain_hub_alias.go` already imports this package, so the
  reverse import cycles.
- Capabilities are registered in ROOT (`contract_supply_chain.go`), not
  here — root owns the router and always links into production.
- Every list route needs a bounded scope or an explicit limit; the
  handler rejects anchorless reads before any store runs.
- The probes never surface unauthorized or stale evidence: nil inventory
  disables the tier, and eligibility runs inside the bound, not after.
- The packet route never touches lane-B packet types directly; it
  composes through `SupplyChainImpactPacketResponder`, which root
  injects. If lane-B moves the envelope to a leaf, this seam collapses
  back to direct calls.

## Related docs

- [HTTP API Reference](../../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../../docs/public/reference/telemetry/index.md)
- [Architecture](../../../../../docs/public/architecture.md)
