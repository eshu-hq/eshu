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
- `line_number` on `package.json`/`composer.json`/`tsconfig*.json` dependency,
  script, and TypeScript-path rows, and on the npm/Composer/NuGet/Pipfile/Swift
  lockfile rows, is the row's real JSON source line (issue #5329), replacing a
  previously fabricated per-section counter. Because `content.CanonicalEntityID`
  hashes `line_number` into the materialized entity's identity, this is a
  one-time content-entity identity migration, live-verified against an
  isolated Postgres + NornicDB stack with an old-binary-then-new-binary
  re-ingest of the same repository:
    - Function rows reach the graph (`node:Function`), and the existing
      generation-stamped canonical retract
      (`go/internal/storage/cypher/canonical_node_writer_retract.go`) cleanly
      reaps the old counter-keyed nodes on the file's next reprocessing — no
      duplicates were observed.
    - A repository is only migrated on its **next actual content change** to
      that file, not merely by the indexer running again: re-running
      `bootstrap-index` against an unchanged repository is a no-op
      (`scopes_collected: 0`) and leaves old-identity nodes/rows in place
      until a real edit triggers reprocessing. This is a transient artifact
      for the graph, but needs no operator action beyond the repository's
      normal commit/ingest cadence.
    - Plain `Variable` rows (`package.json`/`composer.json` dependencies) are
      explicitly excluded from graph materialization
      (`go/internal/projector/canonical_builder.go`, "Plain Variable rows
      remain in the content store/search surface") and live only in Postgres
      `content_entities`. Before this fix that table had no cleanup for
      entity-id changes (it deleted a row only on whole-file deletion), so a
      line shift would have left the old counter-keyed row and the new
      real-line row both present. This change closes that gap with an anti-join
      reap (`reapStaleContentEntities`,
      `go/internal/storage/postgres/content_writer_reap.go`): after upserting a
      file's complete fresh entity set, it deletes that file's rows whose id is
      not in the fresh set, on every reprocess (live-verified: a re-ingest that
      shifted every dependency line kept the row count correct — 15→14 with a
      removed dependency — instead of doubling it, and a no-op third ingest is
      idempotent). Residual, by design: a repository whose JSON file is
      unchanged on disk keeps its old-identity rows until that file's next real
      content change triggers a reprocess. The deeper cleanup — removing
      `line_number` from content-store dependency identity so reordering never
      churns identity — is tracked in #5357.
