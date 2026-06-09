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
9. `collector.go` - bounded scan: pagination, emission, warnings.
10. `metrics.go` - bounded-label telemetry.

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
- MUST NOT add a `cmd/` binary, `collector.Source` runtime wiring, Helm values,
  or scope `CollectorKind` runtime constants in this slice. Those are gated
  follow-ups that require their own reducer and runtime proof.

## Verify

```bash
cd go && go build ./...
cd go && go test ./internal/collector/azurecloud/... ./internal/facts/ -count=1
cd go && golangci-lint run ./internal/collector/azurecloud/... ./internal/facts/
```
