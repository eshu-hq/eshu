# AGENTS.md - internal/collector/sbomruntime guidance

## Read First

1. `README.md` for the collector/runtime boundary.
2. `go/internal/collector/sbomdocument/README.md` for SBOM parser contracts.
3. `go/internal/collector/ociregistry/README.md` before changing OCI referrer
   fetch behavior.
4. `go/internal/reducer/sbom_attestation_attachment_index.go` before changing
   emitted payload keys.

## Invariants

- Do not parse SBOMs inside the OCI collector. OCI owns referrer descriptor
  discovery; this runtime owns document fetching.
- Do not mark parser-emitted SBOM facts verified. Verification belongs in a
  separate attestation/signature fact and reducer classification.
- Do not emit `oci_registry.*` facts from this package.
- Keep source URIs redacted before handing them to parser contexts or fact
  envelopes.
- Keep document identity deterministic: stable IDs must include source record
  identity and document digest, not wall-clock time.

## Verification

Run:

```bash
go test ./internal/collector/sbomruntime ./internal/reducer -run 'Test(ClaimedSource|HTTPProvider|RuntimeSBOMFacts)' -count=1
```

Also run workflow/coordinator tests when changing target config or scheduling.
