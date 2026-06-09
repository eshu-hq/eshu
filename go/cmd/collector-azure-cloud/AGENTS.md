# AGENTS.md - cmd/collector-azure-cloud guidance

## Read First

1. `README.md` - configuration, live-call safety, and deferred scope.
2. `../../internal/collector/azurecloud/azureruntime/AGENTS.md` - the runtime
   this binary wires; emission and provider-seam rules live there.
3. `docs/public/reference/azure-cloud-collector-contract.md` and
   `docs/public/reference/multi-cloud-collector-contract.md` - the contracts.
4. `config.go` - declarative env config; credentials by name only.
5. `service.go` - `collector.Service` construction and provider-seam selection.
6. `main.go` - telemetry bootstrap and hosted service run.

## Hard Rules

- MUST keep the default provider the gated `azureruntime.LiveProviderFactory`.
  The file-backed offline provider is selected ONLY when
  `ESHU_AZURE_FIXTURE_PAGES_JSON` is set, and is for local proof/smoke only.
  NEVER make a live-calling provider the default.
- MUST NOT require live Azure for `go build` or any test.
- MUST reference credentials by NAME only; never read a secret from target
  config and never log a credential value or name.
- MUST keep config declarative and validated; surface invalid config as a
  startup error, not a silent fallback.
- MUST NOT add reducer admission, new fact families, API/MCP readback, claim
  scheduling, Helm values, or chart wiring in this slice. Those are gated
  follow-ups (issue #1998).
- MUST keep every source file under 500 lines.

## Verify

```bash
cd go && go build ./...
cd go && go test ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/... -count=1
cd go && golangci-lint run ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/...
bash scripts/verify-performance-evidence.sh
```
