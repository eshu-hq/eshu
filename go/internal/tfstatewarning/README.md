# Terraform State Warning Classification

## Purpose

`internal/tfstatewarning` owns the closed severity and actionability mapping for
Terraform-state warning facts. It gives collector emission and status readbacks
one shared source of operator meaning without creating a dependency from status
back into collector code.

## Ownership boundary

This package owns only warning classification for stable
`warning_kind`/`reason` pairs. It does not emit facts, parse Terraform state,
read Postgres rows, render JSON, or choose health status.

## Exported surface

See `doc.go` for the godoc contract. The package exports:

- `Classification` - severity/actionability pair for a known warning shape
- severity constants: `SeverityInfo`, `SeverityWarning`, `SeverityBlocking`
- actionability constants for accepted guardrails, provider-schema support,
  blocking evidence, accepted normalization, and source-normalization review
- `Classify(warningKind, reason)` - closed lookup that returns `ok=false` for
  unsupported pairs

## Dependencies

This package uses only the Go standard library. That keeps it safe for
collector, status, query, and storage packages to share.

## Telemetry

This package emits no metrics, spans, or logs. Collectors and status surfaces
own their own observability.

## Gotchas / invariants

- Unknown pairs must stay unknown. A default severity would hide new warning
  shapes under the wrong operational meaning.
- The table must contain only stable, public-safe reason codes. Raw source
  details belong in warning facts after redaction, not in classification.
- Status may use this table to backfill older warning facts that predate
  persisted `severity` and `actionability` fields.

## Related docs

- `go/internal/collector/terraformstate/README.md`
- `go/internal/status/README.md`
- `docs/public/services/collector-terraform-state-operations.md`
