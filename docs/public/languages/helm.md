# Helm Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `helm`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/helm_comprehensive/`
- Unit test suite: `go/internal/parser/engine_yaml_semantics_test.go`,
  `go/internal/parser/yaml/helm_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

A Helm values file is decoded once. The decoded YAML documents feed both the
base `helm_values` extraction (top-level keys, image repositories) and the
Grafana/metric/log/trace observability extraction, rather than each re-parsing
the file source.

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Helm charts (`Chart.yaml`) | `helm-charts-chart-yaml` | supported | `helm_charts` | `name, line_number` | `node:HelmChart` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Chart name, version, app version | `chart-name-version-app-version` | supported | `properties` | `name, line_number, version, app_version` | `property:HelmChart.properties` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Chart dependencies | `chart-dependencies` | supported | `helm_charts` | `name, line_number, dependencies` | `property:HelmChart.list` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm` | Compose-backed fixture verification | - |
| Values files (`values*.yaml`) | `values-files-values-yaml` | supported | `helm_values` | `name, line_number` | `node:HelmValues` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm`; `go/internal/parser/yaml/helm_test.go::TestParseHelmValuesEmitsBaseRowAndGrafanaObservabilityFromSameFile` | Compose-backed fixture verification | - |
| Values top-level keys | `values-top-level-keys` | supported | `helm_values` | `name, line_number, top_level_keys` | `property:HelmValues.top_level_keys` | `go/internal/parser/engine_yaml_semantics_test.go::TestDefaultEngineParsePathYAMLKustomizeAndHelm`; `go/internal/parser/yaml/helm_test.go::TestParseHelmValuesEmitsBaseRowAndGrafanaObservabilityFromSameFile` | Compose-backed fixture verification | - |

## Framework And Library Support

Supported today:

- Helm is deployment configuration evidence, not application-framework
  reachability.
- `Chart.yaml`, chart dependencies, values files, and top-level values keys are
  modeled.

Not claimed today:

- Template rendering, `.Values` reference resolution, Helm hooks, hook weights,
  and post-render Kubernetes resource liveness are not modeled.

## Known Limitations
- Helm template files (`.yaml` in `templates/`) are not parsed for resource definitions
- Values references inside templates (`{{ .Values.key }}`) are not statically resolved
- Helm hooks and weights are not extracted as structured metadata
