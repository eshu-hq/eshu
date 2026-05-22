# internal/collector/terraformstate

## Purpose

`internal/collector/terraformstate` reads approved Terraform state snapshots
from local and S3-like sources, parses them in bounded windows, redacts
sensitive values, resolves provider schema metadata, and emits typed fact
envelopes.

## Ownership boundary

This package owns Terraform-state source identity, discovery resolution,
state-source readers, parser redaction, composite capture, warning facts, and
snapshot identity. It does not schedule workflow claims, choose credentials,
commit facts, write graph rows, or call cloud SDKs outside the source
interfaces supplied by callers.

## Exported surface

See `doc.go` for the package contract. Key surfaces include state source
interfaces, local and S3 source configs, `ParseDiscoveryConfig`,
`DiscoveryResolver`, candidate and snapshot identity helpers, `LocatorHash`,
`ScopeLocatorHash`, `LoadPackagedSchemaResolver`, parser output types, warning
fact construction, and local-candidate policy types.

## Dependencies

The package consumes `internal/facts`, workflow scope identity, telemetry, and
Terraform provider schema metadata from `internal/terraformschema`. Durable
claim and fact persistence belongs to storage and collector services.

## Telemetry

Terraform-state discovery uses `SpanTerraformStateDiscoveryResolve`. Claim-aware
collection records claim wait and fact-emission spans in `internal/collector`.
Schema and composite handling expose bounded metrics such as unknown composite
and resolver-entry counts through the configured instruments.

## Gotchas / invariants

- Raw state bytes must stay inside `StateSource` readers and parser-local
  buffers. Do not persist or log unredacted state values.
- `LocatorHash` includes backend kind, locator, and version ID. `ScopeLocatorHash`
  excludes version ID and must stay aligned with state-snapshot scope IDs.
- `ErrStateNotModified`, `ErrStateMissing`, and `ErrStateTooLarge` are
  distinct operational outcomes.
- Discovery may wait for a Git generation; do not treat that as parser failure.
- Provider schema gaps should emit warnings or bounded counters, not invent
  attribute truth.

## Focused tests

```bash
cd go
go test ./internal/collector/terraformstate -run 'Test.*Discovery|Test.*Identity|Test.*Parser|Test.*Schema|Test.*Source|Test.*Warning' -count=1
go test ./internal/collector/terraformstate -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/collector/README.md`
- `go/internal/terraformschema/README.md`
