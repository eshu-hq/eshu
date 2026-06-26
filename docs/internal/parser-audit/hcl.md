# HCL Parser Audit

## Overview
Parses Terraform (`.tf`) and Terragrunt (`.hcl`) configuration files using the official HashiCorp `hcl/v2` library with its `hclsyntax` native parser — NOT tree-sitter. This is a **declarative configuration** parser. Extracts Terraform blocks (resources, providers, modules, data sources, variables, outputs, locals, backends, imports, moved, removed, checks, lockfile providers), Terragrunt configs (dependencies, inputs, locals, remote states, module sources, include chain), PagerDuty declarations, and Grafana resource metadata. 15 src files, 6 test files. regexp.MustCompile in 2 files.

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
- `TestParseTerragruntHCL` (`parser_test.go:100+`): Terragrunt path detection, config rows
- Parent-level tests (`hcl_terraform_test.go`, `hcl_terragrunt_test.go`, `hcl_terragrunt_join_additional_test.go`, `hcl_terraform_modern_test.go`): comprehensive Terraform block extraction, Terragrunt expression coverage
- `grafana_declarations_test.go`: Grafana folder/dashboard/datasource/rule-group extraction
- `pagerduty_declarations_test.go`: PagerDuty module declaration details
- `include_chain_test.go`: Terragrunt include-chain walking
- `terraform_resource_attributes_test.go`: cty-value attribute extraction for drift
- `terragrunt_remote_state_test.go`: remote state with include-chain resolution

## Unverified / Claimed-but-Untested Constructs
Most claimed constructs have dedicated test files. The package has 6 test files plus 6 parent-level test files. However:
- **Terraform checks block**: claimed in README but may not have a dedicated test (verify in parser_test.go beyond line 100)
- **Provider lockfile parsing**: no separate lockfile test file visible
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
**deep** — 15 src files with 6 package-internal test files plus 6 parent-level test files. Covers Terraform blocks comprehensively, Terragrunt with include chains, PagerDuty/Grafana declarations, resource attribute extraction for drift. Uses the official HashiCorp HCL parser (permanent exception — no tree-sitter needed). This is substantially the most-tested parser in the manifest category.

## Recommended Actions
- Document that HCL is a **permanent exception** — it uses HashiCorp's canonical `hcl/v2` parser, not tree-sitter
- Verify checks block testing coverage (may be in `hcl_terraform_modern_test.go`)
- Verify provider lockfile testing coverage
- Consider a malformed-HCL tolerance test
