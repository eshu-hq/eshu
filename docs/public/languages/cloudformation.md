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
| Condition definitions | `condition-definitions` | supported | `cloudformation_conditions` | `name, line_number, expression` | `node:CloudFormationCondition` | `go/internal/parser/cloudformation/conditions_test.go::TestParseCapturesConditionsAndNestedStackMetadata`, `go/internal/parser/cloudformation/conditions_test.go::TestParseEvaluatesResolvableConditions`, `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | Go now materializes top-level `Conditions` entries as first-class content entities instead of only preserving raw resource/output condition names, and it records evaluated results when the expression is fully resolvable from template-local facts. |
| Export names | `export-names` | supported | `cloudformation_outputs` | `name, line_number, export_name` | `property:export_name property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| Cross-stack imports/exports | `cross-stack-imports-exports` | supported | `cloudformation_cross_stack_imports`, `cloudformation_cross_stack_exports` | `name, line_number` | `node:CloudFormationImport`, `node:CloudFormationExport` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathJSONCloudFormationSAMTransformList`, `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | YAML now keeps the same cross-stack import/export buckets that JSON already preserved, so the parser surface is format-consistent. |
| AllowedValues | `allowedvalues` | supported | `cloudformation_parameters` | `name, line_number, allowed_values` | `property:allowed_values property` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation` | Compose-backed fixture verification | - |
| JSON templates | `json-templates` | supported | `cloudformation_resources` | `name, line_number, file_format` | `node:CloudFormationResource` | `go/internal/parser/cloudformation/parser_test.go::TestParsePersistsFileFormat` | Compose-backed fixture verification | JSON-formatted templates now share the same parser path as YAML and persist `file_format` on CloudFormation rows. |
| Nested stack template URL | `nested-stack-template-url` | supported | `cloudformation_resources` | `name, line_number, resource_type, template_url` | `property:CloudFormationResource.template_url`, `query:entities/{id}/context` | `go/internal/parser/cloudformation/conditions_test.go::TestParseCapturesConditionsAndNestedStackMetadata`, `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCloudFormation`, `go/internal/query/entity_content_cloudformation_fallback_test.go::TestGetEntityContextFallsBackToCloudFormationNestedStackResource`, `go/internal/query/entity_content_cloudformation_fallback_test.go::TestGetEntityContextLinksNestedStackTemplateURLToRepoLocalTemplate`, `go/internal/query/entity_content_cloudformation_fallback_test.go::TestGetEntityContextLeavesRemoteNestedStackTemplateURLUnlinked` | Compose-backed fixture verification | Nested `AWS::CloudFormation::Stack` resources now preserve `TemplateURL`, surface it on the Go entity-context path as a synthesized `DEPLOYS_FROM` relationship, and resolve obvious repo-local nested-stack targets without losing the raw URL when no local match exists. |

## Framework And Library Support

Supported today:

- CloudFormation is infrastructure evidence, not application-framework
  reachability.
- Resources, parameters, outputs, conditions, nested stacks, and cross-stack
  imports/exports are modeled as template evidence.

Not claimed today:

- Intrinsic-function evaluation, deployment-time resource liveness, and
  application framework behavior behind provisioned resources are not modeled.

## Known Limitations
- Intrinsic functions (!Ref, !Sub, !GetAtt) stored as string values, not resolved
- YAML templates report each Parameters/Conditions/Resources/Outputs entity's
  own real, distinct `line_number`, plus a real `end_line` spanning its value
  (an Export inherits its owning Output's position). The YAML adapter walks
  the raw `gopkg.in/yaml.v3` node tree -- anchored strictly at the document
  root's own top-level section pairs, never by searching for a key name
  anywhere in the tree -- so a nested same-named key (for example an
  `AWS::CloudFormation::Stack` resource whose own `Properties` happens to
  contain a `Resources` or `Outputs` key) is never mistaken for a template
  section (issue #5328). Because `line_number` participates in a content
  entity's identity, upgrading to this real per-entity line re-identifies
  every CFN entity in an already-indexed repo on its next snapshot (the old
  entities, previously all sharing the document-root line, are retracted and
  recreated with new ids) -- a one-time, self-healing entity-count churn with
  no data loss.
- JSON templates still report only a single document-level `line_number` for
  every Parameters/Conditions/Resources/Outputs/Exports/cross-stack-import
  entity in the file, and never set `end_line` at all: JSON decoding does not
  preserve each key's own physical line the way the YAML adapter's node walk
  does. Every entity in a JSON CloudFormation template resolves to the same
  line. Extending JSON to the same per-entity precision YAML now has is
  tracked separately in issue #5348 and is not implemented yet.
