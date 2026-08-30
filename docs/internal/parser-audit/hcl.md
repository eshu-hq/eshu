# HCL Parser Audit

## Overview
Parses Terraform (`.tf`) and Terragrunt (`.hcl`) configuration files using the official HashiCorp `hcl/v2` library with its `hclsyntax` native parser — NOT tree-sitter. This is a **declarative configuration** parser. Extracts Terraform blocks (resources, providers, modules, data sources, variables, outputs, locals, backends, imports, moved, removed, checks, lockfile providers), Terragrunt configs (dependencies, inputs, locals, remote states, module sources, include chain), PagerDuty declarations, and Grafana resource metadata. 15 src files, 10 test files (the test count is the number of distinct test files in this package's directory cited under Verified-by-Test below, not every test file in the directory). regexp.MustCompile in 2 files.

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **Terraform resources**: name, type, provider, resource_service, count, for_each
- **Terraform providers**: source, version, alias
- **Terraform modules**: source, version
- **Terraform data sources**: name, type
- **Terraform variables**: name, type, default, description
- **Terraform outputs**: name, description, sensitive
- **Terraform locals**: name, value
- **Terraform backends**: type, config attributes
- **Terraform imports/moved/removed blocks**: resource address, from/to
- **Terraform checks**: name, assertions
- **Terraform lockfile providers**: provider name, version, hashes
- **Terragrunt configs**: source includes, module source
- **Terragrunt dependencies**: dependency blocks
- **Terragrunt inputs**: input assignments
- **Terragrunt locals**: local value assignments
- **Terragrunt remote states**: backend, config, include-chain resolution
- **Terragrunt include warnings**: failed includes
- **PagerDuty declarations**: module source fingerprint, declaration kind, outcome
- **Grafana declarations**: folders, dashboards, datasources, rule groups
- **Resource attribute extraction**: cty-value evaluation for drift comparison
- **Resource attribute drift**: attribute keys, values, sensitive markers

## Verified-by-Test Constructs
- `TestTerraformParseResourceMetadata` (`parser_test.go:16`): resources with count/for_each, provider, resource_service
- `TestTerraformParsePagerDutyDeclarationsFromModules` (`parser_test.go:55`): PagerDuty module declarations, source_class, declaration_kind
- `TestTerragruntParseHelperPaths` (`parser_test.go:248`): Terragrunt path detection, config rows
- External `hcl_test` tests reach the code two ways: `hcl_terraform_test.go` and `hcl_terraform_modern_test.go` drive the parent engine (`parser.DefaultEngine`); `hcl_terragrunt_test.go` and `hcl_terragrunt_join_additional_test.go` call this package's `Parse` directly — the first through `parseTerragruntPayloadForTest`, the second through `parseTerragruntConfigForTest`, which wraps it — never touching the parent engine. Together: comprehensive Terraform block extraction, Terragrunt expression coverage
- `TestDefaultEngineParsePathHCLTerraformModernBlockMetadata` (`hcl_terraform_modern_test.go`): import, moved, removed, and check block metadata
- `TestDefaultEngineParsePathHCLTerraformLockFileProviderMetadata` (`hcl_terraform_modern_test.go`): provider lockfile version, constraints, and hashes
- `grafana_declarations_test.go`: Grafana folder/dashboard/datasource/rule-group extraction
- `pagerduty_declarations_test.go`: PagerDuty module declaration details
- `include_chain_test.go`: Terragrunt include-chain walking
- `terraform_resource_attributes_test.go`: cty-value attribute extraction for drift
- `terragrunt_remote_state_test.go`: remote state with include-chain resolution

## Unverified / Claimed-but-Untested Constructs
Most claimed constructs have dedicated test files. Ten test files are cited below, and all ten now live in this package, since the four Terraform and Terragrunt files among them have moved here. The directory itself holds 13. However:
- **Terragrunt include warnings**: not explicitly tested in isolated form
- **Helm provider resources** (if any special handling)

## Edge Cases Considered
- Terragrunt include chain with multiple levels
- Terraform resources with count/for_each
- PagerDuty module source fingerprinting
- Grafana resource metadata across multiple resource types
- Resource attribute extraction using cty-value (not raw source bytes)
- Multi-element attribute drift (parser-side dedup)
- Terragrunt module sources and dependencies

## Edge Cases NOT Considered
- Malformed HCL syntax
- Empty files
- Files with mixed Terraform and non-Terraform blocks
- Terraform 1.9+ features (if newer than terraformschema knowledge)
- Large heredoc values in attributes
- Terraform functions in expressions (cty evaluation with nil context)

## Verdict
**deep** — 15 src files with 10 test files, all in this package: six declaring `package hcl` and four external `hcl_test` files, of which only the Terraform pair (`hcl_terraform_test.go`, `hcl_terraform_modern_test.go`) drives the parent engine — the Terragrunt pair (`hcl_terragrunt_test.go`, `hcl_terragrunt_join_additional_test.go`) calls this package's `Parse` directly. Counts here are the distinct test files cited under Verified-by-Test that live in this package's directory, not a listing of that directory. Both halves matter: the section cites 10 files at both ends of this change, and before the move only 6 of them lived here, which is the 6 the previous revision reported. Covers Terraform blocks comprehensively, Terragrunt with include chains, PagerDuty/Grafana declarations, resource attribute extraction for drift. Uses the official HashiCorp HCL parser (permanent exception — no tree-sitter needed). This is substantially the most-tested parser in the manifest category.

## Recommended Actions
- Document that HCL is a **permanent exception** — it uses HashiCorp's canonical `hcl/v2` parser, not tree-sitter
- Consider a malformed-HCL tolerance test
