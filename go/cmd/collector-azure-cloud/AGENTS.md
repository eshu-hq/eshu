# AGENTS.md - cmd/collector-azure-cloud guidance

## Read First

1. `README.md` - fixture/claimed-live modes, configuration, live-call safety.
2. `../../internal/collector/azurecloud/azureruntime/AGENTS.md` - the runtime
   this binary wires; emission, claimed-source, and provider-seam rules live there.
3. `docs/public/reference/azure-cloud-collector-contract.md` and
   `docs/public/reference/multi-cloud-collector-contract.md` - the contracts.
4. `config.go` - declarative fixture env config; credentials by name only.
5. `claimed_config.go` - claimed-live instance selection and configuration.
6. `service.go` - `collector.Service`/`collector.ClaimedService` construction.
7. `status_committer.go` - claim-status recording around the durable committer.
8. `azure_live_client.go` - the injectable live Resource Graph provider factory.
9. `main.go` - flag/mode parsing, telemetry bootstrap, hosted service run.

## Hard Rules

- MUST keep fixture mode the default. In fixture mode the file-backed offline
  provider is selected ONLY when `ESHU_AZURE_FIXTURE_PAGES_JSON` is set;
  otherwise the gated `azureruntime.LiveProviderFactory` is used. NEVER make a
  live-calling provider the fixture-mode default.
- MUST keep live transport reachable only via opt-in `-mode claimed-live` with an
  explicit enabled, claim-enabled instance and `live_collection_enabled=true`.
- MUST NOT require live Azure for `go build` or any test; the live provider
  factory is a package var (`newAzureLiveProviderFactory`) so tests inject a
  gated or fixture factory.
- MUST reference credentials by NAME only; never read a secret from config and
  never log a credential value or name. The redaction key comes from a file path
  and is never logged.
- MUST keep config declarative and validated; surface invalid config as a
  startup error, not a silent fallback.
- MUST NOT add reducer admission, new fact families, API/MCP readback, Helm
  values, or chart wiring in this package. Helm activation and live-smoke proof
  are gated follow-ups (issue #3024).
- MUST keep the claim-status committer's metric labels bounded enums only.
- MUST keep every source file under 500 lines.

## Verify

```bash
cd go && go build ./...
cd go && go test ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/... -count=1
cd go && golangci-lint run ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/...
bash scripts/verify-performance-evidence.sh
```
