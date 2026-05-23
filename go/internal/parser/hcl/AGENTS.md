# AGENTS.md - internal/parser/hcl

The HCL adapter owns Terraform, provider-lock, and Terragrunt parser evidence.
Use `README.md` and `doc.go` for the current payload contract.

## Read First

1. `README.md` and `doc.go`.
2. `parser.go`, `terraform_modern.go`, `terraform_backend.go`,
   `terraform_lock.go`, and `terraform_resource_attributes.go`.
3. `terragrunt_remote_state.go`, `include_chain.go`, `helpers.go`,
   `expression_helpers.go`, `values.go`, and `lexical_helpers.go`.
4. Parent tests `../hcl_terraform_test.go`, `../hcl_terragrunt_test.go`, and
   `../hcl_terragrunt_join_additional_test.go`.

## Mandatory Guardrails

- This package MUST NOT import `internal/parser`; parent wrappers own registry,
  runtime, path normalization, and `Engine` signatures.
- `Parse` must preserve Terraform, provider-lock, and Terragrunt bucket names,
  row fields, and deterministic ordering.
- `terragrunt.hcl` uses the Terragrunt path. `.terraform.lock.hcl` uses the
  lockfile path. Other HCL files use Terraform block extraction.
- Terragrunt helper and include-chain extraction is bounded static evidence:
  no broad HCL/Terragrunt expression evaluator, no repository-specific
  conventions, and no symlink/device/FIFO include reads.
- Resource drift attributes use cty evaluation through `literalAttributeValue`;
  raw byte slicing breaks heredocs and escaped strings.
- Keep parser `source_path` for Terragrunt remote-state provenance distinct
  from backend `path`.
- The multi-element first-wins debug log in `walkBlockAttributes` is tied to
  the state-side flatten log and frozen `LogKeyDriftMultiElement*` keys. Do not
  remove or rename it without coordinated storage and telemetry changes.

## Change Scope

- Terraform block changes start with focused parent Terraform tests.
- Terragrunt expression, include, or remote-state changes start with focused
  Terragrunt tests and warning-row coverage for rejection paths.
- Do not reassign HCL extension or exact-name routing without
  architecture-owner approval; it changes fact idempotency.
- Do not move Terraform relationship interpretation here. This package emits
  parser evidence only.
