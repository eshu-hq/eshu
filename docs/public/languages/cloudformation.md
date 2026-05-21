# CloudFormation Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `cloudformation`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/cloudformation_comprehensive/`
- Unit test suite: `go/internal/parser/engine_yaml_semantics_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Resources | `resources` | supported | `cloudformation_resources` | `name, line_number, resources` | `node:CloudFormationResource` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| Parameters | `parameters` | supported | `cloudformation_parameters` | `name, line_number` | `node:CloudFormationParameter` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| Outputs | `outputs` | supported | `cloudformation_outputs` | `name, line_number` | `node:CloudFormationOutput` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| DependsOn | `dependson` | supported | `cloudformation_resources` | `name, line_number, depends_on` | `property:depends_on property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| Conditions | `conditions` | supported | `cloudformation_resources` | `name, line_number, condition` | `property:condition property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| Condition definitions | `condition-definitions` | supported | `cloudformation_conditions` | `name, line_number, expression` | `node:CloudFormationCondition` | `go/internal/parser/cloudformation_support_test.go::TestParseCloudFormationTemplateCapturesConditionsAndNestedStackMetadata`, `go/internal/parser/cloudformation_support_test.go::TestParseCloudFormationTemplateEvaluatesResolvableConditions`, `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | Go now materializes top-level `Conditions` entries as first-class content entities instead of only preserving raw resource/output condition names, and it records evaluated results when the expression is fully resolvable from template-local facts. |
| Export names | `export-names` | supported | `cloudformation_outputs` | `name, line_number, export_name` | `property:export_name property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| Cross-stack imports/exports | `cross-stack-imports-exports` | supported | `cloudformation_cross_stack_imports`, `cloudformation_cross_stack_exports` | `name, line_number` | `node:CloudFormationImport`, `node:CloudFormationExport` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathJSONCloudFormationSAMTransformList`, `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | YAML now keeps the same cross-stack import/export buckets that JSON already preserved, so the parser surface is format-consistent. |
| AllowedValues | `allowedvalues` | supported | `cloudformation_parameters` | `name, line_number, allowed_values` | `property:allowed_values property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| JSON templates | `json-templates` | supported | `cloudformation_resources` | `name, line_number, file_format` | `node:CloudFormationResource` | `go/internal/parser/cloudformation_support_test.go::TestParseCloudFormationTemplatePersistsFileFormat` | Compose-backed fixture verification | JSON-formatted templates now share the same parser path as YAML and persist `file_format` on CloudFormation rows. |
| Nested stack template URL | `nested-stack-template-url` | supported | `cloudformation_resources` | `name, line_number, resource_type, template_url` | `property:CloudFormationResource.template_url`, `query:entities/{id}/context` | `go/internal/parser/cloudformation_support_test.go::TestParseCloudFormationTemplateCapturesConditionsAndNestedStackMetadata`, `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation`, `go/internal/query/entity_content_cloudformation_fallback_test.go::TestGetEntityContextFallsBackToCloudFormationNestedStackResource`, `go/internal/query/entity_content_cloudformation_fallback_test.go::TestGetEntityContextLinksNestedStackTemplateURLToRepoLocalTemplate`, `go/internal/query/entity_content_cloudformation_fallback_test.go::TestGetEntityContextLeavesRemoteNestedStackTemplateURLUnlinked` | Compose-backed fixture verification | Nested `AWS::CloudFormation::Stack` resources now preserve `TemplateURL`, surface it on the Go entity-context path as a synthesized `DEPLOYS_FROM` relationship, and resolve obvious repo-local nested-stack targets without losing the raw URL when no local match exists. |

## Known Limitations
- Intrinsic functions (!Ref, !Sub, !GetAtt) stored as string values, not resolved
