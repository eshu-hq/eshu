# Terragrunt Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `terragrunt`
- Family: `iac`
- Parser: `DefaultEngine (hcl)`
- Entrypoint: `go/internal/parser/hcl_language.go`
- Fixture repo: `tests/fixtures/ecosystems/terragrunt_comprehensive/`
- Unit test suite: `go/internal/parser/engine_infra_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Terragrunt config blocks (`include`, `locals`, `inputs`) | `terragrunt-config-blocks-include-locals-inputs` | supported | `terragrunt_configs` | `name, line_number` | `node:TerragruntConfig` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerragrunt` | Compose-backed fixture verification | - |
| Include block labels | `include-block-labels` | supported | `terragrunt_configs` | `name, line_number, includes` | `property:TerragruntConfig.includes` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerragrunt` | Compose-backed fixture verification | - |
| Dependency blocks | `dependency-blocks` | supported | `terragrunt_dependencies` | `name, line_number, config_path` | `node:TerragruntDependency` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerragruntBuildsFirstClassDependencyLocalAndInputEntities` | Snapshot-backed entity proof | Dependency blocks are first-class content entities in Go and persist as standalone dependency nodes. |
| Locals block | `locals-block` | supported | `terragrunt_locals` | `name, line_number, value` | `node:TerragruntLocal` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerragruntBuildsFirstClassDependencyLocalAndInputEntities` | Snapshot-backed entity proof | Locals are now independently queryable through the Go content/query surface. |
| Inputs block | `inputs-block` | supported | `terragrunt_inputs` | `name, line_number, value` | `node:TerragruntInput` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerragruntBuildsFirstClassDependencyLocalAndInputEntities` | Snapshot-backed entity proof | Inputs are now independently queryable through the Go content/query surface. |
| Source attribute in `terraform` block | `source-attribute-in-terraform-block` | supported | `source-attribute-in-terraform-block` | `terraform_source` | `property:TerragruntConfig.source` and `node:TerraformModule.source` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathHCLTerragrunt` | Compose-backed fixture verification | The Terragrunt `terraform.source` value now also materializes through the normal `TerraformModule` surface. |

## Current Truth
- The current Go parser covers the documented Terragrunt contract end to end.
- Repository-context read surfaces now also include `dependency.config_path`, `read_terragrunt_config`, `include`, `file`, `templatefile`, `*.tfvars`, and local module-source paths, while `terraform.source` also materializes through `TerraformModule`.
- `read_terragrunt_config()` itself remains opaque in parser output.
- Terragrunt entities are not returned as source-code dead-code candidates.
  The code dead-code analysis reports HCL as `non_code_iac_evidence`; Terragrunt
  liveness belongs to configuration provenance, relationship evidence, and
  infrastructure query surfaces.

## Dead-code Support

Terragrunt/HCL dead-code support is `non_code_iac_evidence`. The parser records
Terragrunt configs, dependency blocks, locals, inputs, Terraform module source
evidence, and bounded include-chain remote-state evidence for graph and query
surfaces, but the code dead-code endpoint does not treat those entities as
cleanup candidates.

Exact cleanup-safe Terragrunt liveness remains blocked by runtime include
resolution, dependency graph selection, workspace and variable selection,
Terraform plan/state availability, and dynamic Terraform block expansion.

## Known Limitations
- `read_terragrunt_config()` calls remain opaque expression text.
- HCL function calls within `locals` are not evaluated; values are captured as raw text.
