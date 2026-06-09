# AGENTS.md - cmd/collector-gcp-cloud guidance

## Read First

1. `README.md` - what the binary does and why it is fixture-driven only.
2. `../../internal/collector/gcpcloud/gcpruntime/AGENTS.md` - the source and
   PageProvider seam contract.
3. `docs/public/reference/gcp-cloud-collector-contract.md` - scope/generation,
   payload boundary, telemetry, and the deferred-slice boundary.
4. `main.go` - flag parsing, telemetry/pprof/Postgres bootstrap, service run.
5. `config.go` - declarative file config parsing and fixture-file mapping.
6. `service.go` - source and committer wiring.
7. `status_committer.go` - claim-status recording around the durable committer.

## Invariants

- This binary performs no live Google Cloud call. The page provider is always
  the offline `FixturePageProvider`. Do not wire `gcpruntime.LiveClient` here.
- Reference credentials by NAME only (`credential_ref`). Never read, store, or
  log credential material or names. The redaction key comes from a file path and
  is never logged.
- Do not add `ESHU_*` environment variables, Helm values, chart claims, or
  reducer/API readback in this slice. Those are deferred slices; adding them
  implies a runtime readiness promise the contract forbids.
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
