# Terraform Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `terraform`
- Family: `iac`
- Parser: `DefaultEngine (hcl)`
- Entrypoint: `go/internal/parser/hcl_language.go`
- Fixture repo: `tests/fixtures/ecosystems/terraform_comprehensive/`
- Unit test suite: `go/internal/parser/engine_infra_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Resource blocks | `resource-blocks` | supported | `terraform_resources` | `name, line_number, resource_type, resource_name` | `node:TerraformResource` | `go/internal/parser/hcl_terraform_test.go::TestDefaultEngineParsePathHCLTerraformResourceMultiplicityMetadata` | Compose-backed fixture verification | Resource rows now preserve raw `count` and `for_each` expressions when present, alongside the block identity fields. |
| Variable blocks | `variable-blocks` | supported | `terraform_variables` | `name, line_number` | `node:TerraformVariable` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerraform` | Compose-backed fixture verification | - |
| Output blocks | `output-blocks` | supported | `terraform_outputs` | `name, line_number` | `node:TerraformOutput` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerraform` | Compose-backed fixture verification | - |
| Module blocks | `module-blocks` | supported | `terraform_modules` | `name, line_number` | `node:TerraformModule` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerraform` | Compose-backed fixture verification | - |
| Data source blocks | `data-source-blocks` | supported | `terraform_data_sources` | `name, line_number` | `node:TerraformDataSource` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerraform` | Compose-backed fixture verification | - |
| `terraform {}` block metadata | `terraform-block-metadata` | supported | `terraform_blocks` | `name, line_number, required_providers` | `node:TerraformBlock` | `go/internal/parser/hcl_terraform_test.go::TestDefaultEngineParsePathHCLTerraformBlockMetadata` | Compose-backed fixture verification | The native HCL parser now materializes a standalone `terraform {}` block row that carries required-provider metadata and provider-source summaries. |
| Provider blocks | `provider-blocks` | supported | `terraform_providers` | `name, line_number` | `node:TerraformProvider` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerraform` | Compose-backed fixture verification | - |
| Locals blocks | `locals-blocks` | supported | `terraform_locals` | `name, line_number, value` | `node:TerraformLocal` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerraform` | Compose-backed fixture verification | - |

## Current Truth
- The current Go runtime ships the documented Terraform parser surface, including first-class `terraform {}` block entities and provider-schema-backed relationship extraction.
- The first-class `terraform_blocks` surface persists a standalone `terraform {}` entity with provider metadata.
- The packaged Terraform provider schemas are still intentionally present
  because the Go runtime uses them for schema-driven relationship extraction.
  They are part of the current relationship path.
- Terraform entities are not returned as source-code dead-code candidates.
  The code dead-code analysis reports HCL as `non_code_iac_evidence`; Terraform
  liveness belongs to infrastructure evidence, relationship evidence, state or
  plan comparison, and repository-context surfaces.

## Dead-code Support

Terraform/HCL dead-code support is `non_code_iac_evidence`. The parser
extracts Terraform blocks, resources, modules, providers, backends, imports,
moved blocks, removed blocks, checks, and lockfile providers for graph and
query evidence, but the code dead-code endpoint does not treat those entities
as cleanup candidates.

Exact cleanup-safe Terraform liveness remains blocked by Terraform plan/state
availability, module reference graph resolution, workspace and variable
selection, dynamic block expansion, and Terragrunt runtime include resolution.

## Known Limitations
- `count` and `for_each` meta-arguments are captured on resource rows, but are not expanded to model multiple resource instances
- `dynamic` blocks within resources are not traversed for nested attribute extraction
- Cross-file variable references (`var.name`, `module.name.output`) are not resolved at parse time
