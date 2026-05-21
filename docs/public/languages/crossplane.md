# Crossplane Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `crossplane`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/crossplane_comprehensive/`
- Unit test suite: `go/internal/parser/engine_yaml_semantics_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Composite Resource Definitions (XRDs) | `composite-resource-definitions-xrds` | supported | `crossplane_xrds` | `name, line_number` | `node:CrossplaneXRD` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| XRD group, kind, version | `xrd-group-kind-version` | supported | `properties` | `name, line_number, kind, group, claim_kind` | `property:XRD.properties` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Compositions | `compositions` | supported | `crossplane_compositions` | `name, line_number` | `node:CrossplaneComposition` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Composition composite type ref | `composition-composite-type-ref` | supported | `crossplane_compositions` | `name, line_number, composite_api_version, composite_kind` | `property:Composition.composite_ref` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Composition resources list | `composition-resources-list` | supported | `crossplane_compositions` | `name, line_number, resource_count, resource_names` | `property:Composition.resources` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Claims | `claims` | supported | `crossplane_claims` | `name, line_number` | `node:CrossplaneClaim` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Claim API version | `claim-api-version` | supported | `crossplane_claims` | `name, line_number, api_version` | `property:Claim.api_version` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |

## Known Limitations
- Composition patch transforms are not modeled as graph edges
- XRD validation schema details are not extracted
- Usage of Composition Functions (pipeline steps) is not captured as structured nodes
