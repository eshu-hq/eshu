# HCL Parser

## Purpose

This package parses Terraform and Terragrunt HCL into the payload buckets used
by the parent parser engine. It owns HCL syntax handling, Terraform block
extraction, resource attribute flattening for drift comparison, provider
lockfile rows, Terragrunt helpers, and Terragrunt `remote_state` evidence from
bounded include chains.

## Ownership Boundary

`internal/parser/hcl` is a language adapter. It does not own parser registry
dispatch, grammar lifecycle, parse timing, repo path normalization, fact
emission, or downstream drift decisions. Those stay in `internal/parser`,
collector code, and reducer/projector packages.

## Exported Surface

See `doc.go` for the godoc contract. The package exports `Parse`, which reads
one HCL file and returns deterministic Terraform and Terragrunt payload rows.

## Telemetry

Parent parser instrumentation records file parse duration. This package only
uses `slog.Default()` for duplicate multi-element nested-block debug records
with `LogKeyDriftMultiElementPrefix` and `LogKeyDriftMultiElementSource`.

## Gotchas / Invariants

- `.terraform.lock.hcl` provider blocks produce lockfile provider rows, not
  provider configuration rows.
- Terragrunt include walking is bounded by depth, cycle detection, regular-file
  checks, and a 1 MiB include-file limit; failures become
  `terragrunt_include_warnings` rows.
- Terragrunt `remote_state.source_path` is parser provenance and must not
  collide with a local backend's `path` attribute.
- Terraform resource attributes use cty evaluation so heredocs and escaped
  strings line up with Terraform-state drift evidence.
- Payload rows are sorted before return so retries and repairs converge.

## Focused Tests

```bash
cd go
go test ./internal/parser/hcl -count=1
go run ./cmd/eshu docs verify ../go/internal/parser/hcl --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `go/internal/parser/README.md`
- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
