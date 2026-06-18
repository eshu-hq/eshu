# AGENTS.md - cmd/collector-gcp-cloud guidance

## Read First

1. `README.md` - what the binary does in fixture and claimed-live modes.
2. `../../internal/collector/gcpcloud/gcpruntime/AGENTS.md` - the source and
   PageProvider seam contract.
3. `docs/public/reference/gcp-cloud-collector-contract.md` - scope/generation,
   payload boundary, telemetry, and the remaining chart/smoke gates.
4. `main.go` - flag parsing, telemetry/pprof/Postgres bootstrap, service run.
5. `config.go` - declarative file config parsing and fixture-file mapping.
6. `claimed_config.go` - claim-driven environment and collector instance
   parsing.
7. `service.go` - source and committer wiring.
8. `status_committer.go` - claim-status recording around the durable committer.

## Invariants

- Fixture mode remains the default and performs no live Google Cloud call. It
  always uses the offline `FixturePageProvider`.
- Claimed-live mode is opt-in only (`-mode claimed-live`) and must run through
  `collector.ClaimedService`, workflow claims, and explicit
  `live_collection_enabled=true` collector instance configuration before wiring
  `gcpruntime.LiveClient`.
- Reference credentials by NAME only (`credential_ref`). Never read, store, or
  log credential material or names. The redaction key comes from a file path and
  is never logged.
- Do not add Helm values, chart claims, reducer/API readback, or live smoke
  claims in this command package. Those remain separate gated slices.
- Keep the status committer's recorded metric labels bounded enums only.
- Keep every source file under 500 lines.

## Verification

```bash
cd go && go build ./...
cd go && go test ./cmd/collector-gcp-cloud/... ./internal/collector/gcpcloud/... -count=1
cd go && golangci-lint run ./cmd/collector-gcp-cloud/... ./internal/collector/gcpcloud/...
scripts/verify-package-docs.sh
scripts/verify-performance-evidence.sh
```
