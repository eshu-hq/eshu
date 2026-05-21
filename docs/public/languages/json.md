# JSON Config Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `json`
- Family: `language`
- Parser: `DefaultEngine (json)`
- Entrypoint: `go/internal/parser/json_language.go`
- Fixture repo: `tests/fixtures/ecosystems/json_comprehensive/`
- Unit test suite: `go/internal/parser/json_language_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| package.json dependencies | `package-json-dependencies` | supported | `variables` | `name, line_number, value, section` | `node:Variable` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathJSONPackageJSON` | Compose-backed fixture verification | - |
| package.json scripts | `package-json-scripts` | supported | `functions` | `name, line_number, source` | `node:Function` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathJSONPackageJSON` | Compose-backed fixture verification | - |
| composer.json require sections | `composer-json-dependencies` | supported | `variables` | `name, line_number, value, section` | `node:Variable` | `go/internal/parser/json_language_test.go::TestDefaultEngineParsePathJSONPreservesDocumentOrderForMetadataAndConfigBuckets` | Compose-backed fixture verification | - |
| tsconfig targeted metadata | `tsconfig-targeted-metadata` | supported | `variables` | `name, line_number, value, config_kind` | `node:Variable` | `go/internal/parser/json_language_test.go::TestDefaultEngineParsePathJSONPreservesDocumentOrderForMetadataAndConfigBuckets` | Compose-backed fixture verification | - |
| Generic JSON metadata only | `generic-json-metadata-only` | partial | `json_metadata` | `top_level_keys` | `property:File` | `go/internal/parser/json_language_test.go::TestDefaultEngineParsePathJSONPreservesDocumentOrderForMetadataAndConfigBuckets` | Compose-backed fixture verification | Arbitrary JSON files stay intentionally quiet to avoid graph noise. |
| JSON CloudFormation templates | `cloudformation-json-delegation` | supported | `cloudformation_resources` | `name, line_number, file_format` | `node:CloudFormationResource` | `go/internal/parser/cloudformation_support_test.go::TestParseCloudFormationTemplatePersistsFileFormat` | Compose-backed fixture verification | JSON CloudFormation now shares the same parser path as YAML and persists `file_format` on CloudFormation rows. |

## Known Limitations
- Generic JSON files emit metadata only and do not expand arbitrary nested objects into graph nodes
- Lockfiles and minified JSON assets are intentionally kept metadata-only to avoid graph noise
