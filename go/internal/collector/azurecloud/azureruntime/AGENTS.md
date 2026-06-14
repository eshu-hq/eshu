# AGENTS.md - internal/collector/azurecloud/azureruntime guidance

## Read First

1. `README.md` - package purpose, delivered scope, and deferred follow-ups.
2. `../README.md` and `../AGENTS.md` - the parent `azurecloud` fact engine this
   runtime drives. Emission, redaction, identity, and telemetry rules live
   there; do not duplicate them here.
3. `docs/public/reference/azure-cloud-collector-contract.md` - the provider
   contract. The runtime is now in-scope, and fixture-backed resource-change
   emission is allowed behind `SourceLaneResourceChanges`, but reducer
   admission, API/MCP readback, and Helm/chart wiring remain gated.
4. `docs/public/reference/multi-cloud-collector-contract.md` - shared
   scope/generation, redaction, and telemetry rules.
5. `config.go` - declarative `Config`/`TargetConfig`; credentials by name only.
6. `provider.go` - `PageProviderFactory` seam, `FixturePageProvider`, and the
   gated `LiveProviderFactory`.
7. `source.go` - `collector.Source` implementation, scope/generation identity,
   and bounded per-target telemetry.

## Hard Rules

- MUST keep the live Azure Resource Graph/ARM client behind
  `PageProviderFactory`. `LiveProviderFactory` MUST stay inert
  (`ErrLiveProviderGated`) until a real read-only adapter lands with its own
  credential, quota, throttle, and fixture proof. NEVER make a live-calling
  factory the default.
- MUST NOT call Azure from any test or non-injected code path.
- MUST reference credentials by NAME only (`CredentialRef`); never inline a
  secret and never log a credential value or name.
- MUST keep telemetry labels, span attributes, and log keys bounded enums or
  non-identifying counts. NEVER add ARM IDs, subscription/tenant IDs, resource
  group/resource names, locations, tags, KQL text, URLs, or credential names.
- MUST keep generation identity deterministic so replayed sweeps converge
  (idempotent re-emission).
- MUST treat partial subscription/management-group access as explicit
  `azure_collection_warning` evidence, never silent success.
- MUST NOT add reducer admission, new fact families, API/MCP readback, Helm
  values, env wiring, or shared-registry telemetry in this package. The existing
  `azure_resource_change` fact kind may be emitted only through fixture-backed
  `SourceLaneResourceChanges`; it must not call Azure or admit graph truth.
- MUST apply TDD and keep every source file under 500 lines.

## Verify

```bash
cd go && go build ./...
cd go && go test ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/... -count=1
cd go && golangci-lint run ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/...
bash scripts/verify-performance-evidence.sh
```
