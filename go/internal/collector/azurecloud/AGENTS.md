# AGENTS.md - internal/collector/azurecloud guidance

## Read First

1. `README.md` - package purpose, delivered scope, and documented follow-ups.
2. `docs/public/reference/azure-cloud-collector-contract.md` - the provider
   contract this package implements. It is gated: do not claim Azure runtime,
   Helm values, environment variables, or query support before they are
   implemented and fixture-proven.
3. `docs/public/reference/multi-cloud-collector-contract.md` - shared provider
   fact, scope/generation, redaction, and telemetry rules.
4. `types.go` - `CollectorKind`, scope-kind and source-lane enums, `Boundary`,
   `ResourceObservation`, and `WarningObservation`.
5. `armid.go` - ARM resource ID normalization (`ParseARMIdentity`).
6. `redaction.go` - extension drop-policy and `RedactionPolicyVersion`.
7. `envelope.go` - durable fact-envelope construction and validation.
8. `resourcegraph.go` - Resource Graph response parsing.
9. `resourcechanges.go` - Resource Graph `resourcechanges` fixture parsing.
10. `collector.go` - bounded scan: pagination, emission, warnings.
11. `metrics.go` - bounded-label telemetry.

## Hard Rules

- MUST keep emission provider-specific. Do NOT introduce a generic
  `cloud_resource` source fact. Reducers admit shared identity.
- MUST keep raw ARM resource IDs in facts and add normalized fields; never
  replace raw identity.
- MUST drop forbidden extension keys (secrets, deployment templates, connection
  strings, access keys, tokens, IPs, private endpoint hostnames, response
  bodies). Extend `azureForbiddenExtensionTokens` and add a redaction test
  before persisting a new extension field family.
- MUST keep telemetry labels bounded enums only. NEVER add ARM IDs,
  subscription/tenant IDs, resource group/resource names, locations, tags, KQL
  text, URLs, or credential names as labels. The metrics test asserts this.
- MUST keep stable fact keys derived from normalized identity (not volatile
  extension or tag values) so idempotent re-emission converges.
- MUST treat partial scope, permission-hidden subscriptions, and truncation as
  explicit warning evidence, never silent success.
- MUST apply TDD: add a fixture and a failing test before new emission
  behavior.
- MUST keep every source file under 500 lines; split before the cap.
- MUST keep this package the pure fixture-driven fact engine. The
  `collector.Source` implementation, the `cmd/collector-azure-cloud` binary, and
  the `PageProviderFactory` seam live in the sibling `azureruntime` package
  (`go/internal/collector/azurecloud/azureruntime`); see its `AGENTS.md`.
- MUST NOT add Helm values, chart wiring, claim-driven workflow scheduling, a
  live-calling default provider, reducer admission, or new fact-kind envelope
  builders. Those remain gated follow-ups (issue #1998). The fixture-backed
  resource-change source lane may emit existing `azure_resource_change` facts;
  it must stay provenance-only and must not admit graph truth.

## Verify

```bash
cd go && go build ./...
cd go && go test ./internal/collector/azurecloud/... ./internal/facts/ -count=1
cd go && golangci-lint run ./internal/collector/azurecloud/... ./internal/facts/
```
