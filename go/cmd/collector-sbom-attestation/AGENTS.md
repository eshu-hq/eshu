# AGENTS.md - collector-sbom-attestation guidance

## Read First

1. `README.md` for the hosted runtime contract.
2. `go/internal/collector/sbomruntime/README.md` for parser and OCI boundaries.
3. `go/internal/workflow/README.md` before changing claim fields.

## Invariants

- Select only enabled, claim-enabled `sbom_attestation` collector instances.
- Do not place credential values in source URIs or logs. Resolve credentials
  from environment variable names in the collector instance config.
- Keep heartbeat interval strictly below claim lease TTL.
- The command wires `collector.ClaimedService`; it does not bypass the workflow
  control store or claim fencing.

## Verification

Run:

```bash
go test ./cmd/collector-sbom-attestation ./internal/collector/sbomruntime -count=1
```
