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
| Generic JSON metadata only | `generic-json-metadata-only` | partial | `json_metadata` | `top_level_keys` | `-` | `go/internal/parser/json_language_test.go::TestDefaultEngineParsePathJSONPreservesDocumentOrderForMetadataAndConfigBuckets` | Compose-backed fixture verification | Arbitrary JSON files stay intentionally quiet to avoid graph noise; `json_metadata.top_level_keys` is parsed but not exposed as a queryable graph node, matching the JSON Config row's Query Surfacing `-` in [Support Maturity](support-maturity.md). |
| JSON CloudFormation templates | `cloudformation-json-delegation` | supported | `cloudformation_resources` | `name, line_number, file_format` | `node:CloudFormationResource` | `go/internal/parser/cloudformation_support_test.go::TestParseCloudFormationTemplatePersistsFileFormat` | Compose-backed fixture verification | JSON CloudFormation now shares the same parser path as YAML and persists `file_format` on CloudFormation rows. |

## Framework And Library Support

Supported today:

- JSON does not claim framework runtime support.
- `package.json`, `composer.json`, `tsconfig`, generic JSON metadata, and JSON
  CloudFormation templates are modeled as configuration evidence.

Not claimed today:

- Arbitrary nested JSON objects, lockfiles, minified assets, and package-manager
  runtime semantics are not expanded into framework reachability truth.

## Known Limitations
- Generic JSON files emit metadata only and do not expand arbitrary nested objects into graph nodes
- `json_metadata.top_level_keys` is parsed and returned in the parser payload, but it is not queryable: no reducer, content materializer, or query/MCP reader consumes it today. There is no `property:File` graph surface for this capability. The properties/`Property` bucket this row previously implied it fed is unimplemented repository-wide (no emitter, reducer, or query reader for `:Property` nodes; tracked as issue #5341) and is out of scope for this fix. The `generic-json-metadata-only` row's `partial` status is parser-proof only; it does not have the consumer proof [Support Maturity](support-maturity.md)'s promotion rule requires for `partial`, and is recorded here rather than silently passing as compliant
- `json_metadata.top_level_keys` is preserved in JSON source order for every JSON file. Manifest files that also emit source-ordered dependency, script, or TypeScript path rows (`package.json`, `composer.json`, `tsconfig*.json`) retain nested key order through a full ordered decode; every other file derives the top-level key order alone through a lighter key-order scan. Both paths yield identical `top_level_keys`; the distinction is an internal cost optimization, not a change in emitted evidence
- npm `package-lock.json`, Composer `composer.lock`, NuGet `packages.lock.json`, Pipenv `Pipfile.lock`, and SwiftPM `Package.resolved` are parsed into exact-version dependency rows where the source file proves registry-style package identity and version; Pub `pubspec` coverage is YAML-owned. The per-ecosystem coverage map for supply-chain impact lives in [Dependency And Lockfile Coverage](../reference/dependency-coverage.md)
- Minified JSON assets are intentionally kept metadata-only to avoid graph noise
