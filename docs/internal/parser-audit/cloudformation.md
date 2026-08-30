# CloudFormation Parser Audit

## Overview
Parses CloudFormation and SAM template evidence from already-decoded YAML/JSON documents. This is a **declarative data** parser — it receives an already-decoded map from the JSON or YAML parent adapter, evaluates bounded condition expressions (Ref, Equals, literal comparison), and extracts resource, parameter, output, condition, import, and export buckets. 5 src files, 4 test files.

## Claimed Constructs
From `doc.go`, `README.md`, `parser.go`:
- **Resources**: name, type (`AWS::*::*`), condition, evaluated condition value, DependsOn, TemplateURL (nested stacks)
- **Parameters**: name, type (defaults to `String`), default value, description, allowed values
- **Outputs**: name, export name
- **Conditions**: name, expression text, evaluated value (resolved/unresolved)
- **Cross-stack Imports**: Fn::ImportValue references collected from Resources
- **Cross-stack Exports**: Output.Export.Name
- **SAM template detection**: via `Transform: AWS::Serverless-2016-10-31` (string or list)
- **Template recognition**: via `AWSTemplateFormatVersion`, SAM transform, or `AWS::*::*` resource types
- **File format preservation**: `file_format` field (`json` or `yaml`)

## Verified-by-Test Constructs
- `TestIsTemplateDetectsSAMTransformList` (`parser_test.go:8`): SAM transform as list element recognized
- `TestParseDefaultsParameterTypeToString` (`parser_test.go:27`): Parameters default type String, name, file_format
- `TestParsePersistsFileFormat` (`parser_test.go:53`): File format preserved on params, resources, outputs, exports; AllowedValues; DependsOn; Export name
- `TestParseCapturesConditionsAndNestedStackMetadata` (`conditions_test.go:8`): Conditions name/expression, resource condition/template_url
- `TestParseEvaluatesResolvableConditions` (`conditions_test.go:48`): Resolved conditions (Fn::Equals with Ref), evaluated values, unresolved condition
- `TestIsTemplateDetectsSAMResourceTypeWithoutTransform` (`imports_test.go:8`): SAM resource-type recognition without a Transform field
- `TestParseCollectsCrossStackImports` (`imports_test.go:24`): cross-stack `Fn::ImportValue` collection
- `positions_test.go`: source-position attachment for parameters, resources, outputs, and exports
- Parent-level: `engine_infra_test.go` verifies JSON attachment; YAML attachment is
  verified by `go/internal/parser/yaml/engine_yaml_semantics_test.go`, which moved into
  the yaml package in issue #6062

## Unverified / Claimed-but-Untested Constructs
- **Nested map-style condition evaluation beyond Fn::Equals**: only Fn::Equals with Ref vs literal is tested
- **TemplateURL extraction**: only tested for nested CloudFormation::Stack resources

## Edge Cases Considered
- Parameter with no explicit Type defaults to `String` (`TestParseDefaultsParameterTypeToString`)
- SAM transform as list (not just string) in `Transform` field
- SAM resource recognition without a Transform field
- Cross-stack `Fn::ImportValue` collection
- Both `json` and `yaml` file formats
- Resolved vs unresolved conditions (evaluated_value/condition_value)
- AllowedValues list preservation
- DependsOn as both string and list forms
- Empty imports when no Fn::ImportValue exists

## Edge Cases NOT Considered
- Malformed documents (e.g., Resources is not a map)
- Empty parameters/resources/outputs sections
- Multiple-depths of nested condition evaluation (e.g., Fn::And, Fn::Or, Fn::Not)
- Custom resource types (non-AWS prefix)
- Non-string parameter types (Number, List<Number>, etc.)
- Path vs line_number in multi-document YAML
- Intrinsic functions: Fn::Sub, Fn::Join, Fn::Select, Fn::FindInMap for property extraction

## Verdict
**moderate** — Tests cover the core structural extraction (parameters, resources, conditions, imports, outputs, exports, file format) and the happy-path of Fn::Equals condition evaluation. As a declarative data parser receiving pre-decoded documents, moderate coverage is appropriate. The parser delegates document decoding to JSON/YAML parents and focuses on bucket extraction.

## Recommended Actions
- Add at least one malformed-document test (empty sections, non-map Resources)
- Document that CloudFormation is a **permanent exception** in the parser taxonomy — it uses bounded structural evaluation over decoded documents, not tree-sitter
- Consider testing non-string parameter types and Fn::And/Fn::Or/Fn::Not condition evaluation
