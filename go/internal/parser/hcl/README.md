# HCL Parser

## Purpose

This package owns Terraform and Terragrunt HCL parsing for the parser engine.
It reads HCL source, extracts Terraform blocks, resources, variables, outputs,
modules, providers, data sources, locals, backends, imports, moved blocks,
removed blocks, checks, lockfile providers, Terragrunt configs, dependencies,
inputs, and local config asset paths, then returns the parser payload shape.

## Ownership boundary

The package is responsible for HCL syntax parsing and language-specific payload
rows. The parent `internal/parser` package still owns registry dispatch, engine
path parsing, repo path normalization, parse timing, and final content metadata
inference.

## Exported surface

The godoc contract is in `doc.go`. Current export:

- `Parse` reads one HCL file and returns the Terraform/Terragrunt payload
  buckets used by the parent parser.

## Dependencies

This package imports `internal/parser/shared` for shared parser options, source
reading, base payload construction, bucket appends, and deterministic bucket
sorting. It imports `internal/terraformschema` only for Terraform resource type
classification. It must not import the parent `internal/parser` package.

## Telemetry

This package emits no telemetry directly. File parse timing remains owned by
the parent parser engine through `eshu_dp_file_parse_duration_seconds`.

## Gotchas / invariants

`terragrunt.hcl` is treated as Terragrunt. `.terraform.lock.hcl` is treated as
a provider lockfile, so its `provider` blocks produce `terraform_lock_providers`
instead of `terraform_providers`. Other HCL files use the Terraform block path.

Terragrunt local config asset extraction is intentionally bounded to static
string, join, lookup, file, templatefile, and local interpolation shapes already
covered by HCL-focused parser tests.

Payload buckets must stay deterministic. Rows are sorted before `Parse`
returns so ingestion retries and repair runs converge on the same facts.

## Related docs

- `go/internal/parser/README.md`
- `docs/docs/architecture.md`
- `docs/docs/reference/local-testing.md`
