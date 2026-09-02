# YAML Parser Audit

## Overview
Parses YAML-family configuration files using `gopkg.in/yaml.v3`. This is a **declarative data** parser — NOT a language parser. Extracts Kubernetes resources, Argo CD applications/ApplicationSets, Crossplane resources (XRDs, compositions, claims), Kustomize overlays, Helm chart/values metadata, CloudFormation/SAM templates (delegated to cloudformation package), Pub dependencies, and metadata-only observability rows from Helm values, Grafana, Prometheus/Mimir, Loki, Tempo, OTel pipelines, Promtail, and Argo CD status resources. 15 src files, 14 test files cited below. One `regexp.MustCompile` (`helm_template_values.go:25`, for `.Values.*` references).

## Claimed Constructs
From `doc.go`, `README.md`, `language.go`:
- **Kubernetes resources**: qualified_name (namespace/kind/name), container_images, apiVersion, kind, metadata
- **Argo CD applications**: source_repo, source_path, source_revision, source_root, positional source tuple fields, destination, sync policy
- **Argo CD ApplicationSets**: generator and template evidence
- **Crossplane XRDs**: apiGroup, kind, names
- **Crossplane compositions**: compositeTypeRef, resources
- **Crossplane claims**: kind, apiGroup, namespace
- **Kustomize overlays**: resources, patches, namespace
- **Helm charts**: name, version, appVersion, apiVersion from Chart.yaml
- **Helm values**: metadata from values.yaml
- **CloudFormation/SAM**: delegated to cloudformation package
- **Pub dependencies**: dart/pub pubspec.yaml and pubspec.lock
- **Observability (metadata-only)**: Helm Grafana folder/dashboard/datasource/alert, Prometheus Operator scrape/rule resources, Prometheus/Mimir Helm values, Promtail client routes, OTel metric/log/trace pipelines, Loki gateway values, Tempo gateway values, Grafana Tempo datasource links, chart ServiceMonitor settings
- **Applied observability**: Argo CD Application status resources, Kubernetes API-exported observability resources
- **Templating sanitization**: Jinja/Helm template normalization for parser-safe reads

## Verified-by-Test Constructs
- `TestParseKubernetesResourceDirectly` (`language_test.go:13`): k8s_resources with qualified_name, container_images, source preservation
- `TestParseCloudFormationIntrinsicDirectly` (`language_test.go:55`): CloudFormation resource, parameter, and output extraction from YAML
- `observability_test.go`: Helm Grafana observability from values.yaml
- `observability_metrics_test.go`: OTel metric pipeline and Prometheus scrape configs
- `observability_log_routes_test.go`: Promtail client routes and OTel log pipelines
- `observability_trace_routes_test.go`: OTel trace pipelines and Tempo gateway values
- `observability_applied_test.go`: Argo CD status resources and API-exported observability
- `pubspec_test.go`: Pub dependency extraction from pubspec.yaml/pubspec.lock
- `engine_yaml_semantics_test.go`: Argo CD Application, Application multi-source, ApplicationSet nested sources, ApplicationSet generator/template sources, and YAML CloudFormation attachment
- `engine_yaml_semantics_crossplane_test.go`: Crossplane XRD, composition, and claim rows
- `engine_yaml_semantics_kustomize_test.go`: Kustomize overlays, patch targets, typed deploy refs, image overrides, and Helm chart/values metadata
- `engine_yaml_flux_semantics_test.go`: Flux Kustomization/HelmRelease/HelmRepository misroute regressions and the generic-group non-regression
- `engine_yaml_flux_fixture_negatives_test.go`, `engine_yaml_flux_helm_fixture_negatives_test.go`: dangling, unknown-kind, OCI, Bucket, chartRef, and generateName sourceRef fixtures captured verbatim
- `engine_kubernetes_semantics_test.go`: Kubernetes qualified_name and container image rows
- `TestParseMultiDocumentYAMLWithAnchorsAndMergeKeys` (`language_test.go:95`): multi-document decoding plus anchor, alias, and merge-key resolution
- `TestDecodeDocumentsPreservesQuotedMergeKey`, `TestDecodeDocumentsRejectsInvalidMergeValue`, and `TestDecodeDocumentsRejectsRecursiveAlias` (`language_test.go:146`, `language_test.go:176`, `language_test.go:193`): quoted merge keys and invalid or recursive alias failures
- Parent-level: `engine_infra_test.go` is among the parent tests that still drive YAML through the parent engine. The Engine tests listed above moved into this package in issue #6062; they run as external `yaml_test`. The CloudFormation real-line tests that used to sit beside it at the parser root moved to `go/internal/parser/cloudformation/engine_yaml_cloudformation_lines_test.go` (issue #6062); they run as external `cloudformation_test` and still drive the YAML adapter through the parent engine.

## Unverified / Claimed-but-Untested Constructs
- **Helm values.yaml non-Grafana observability**: Prometheus Operator scrape/rule resources, Loki gateway values not explicitly listed in test file names
- **Helm template manifests**: skip behavior tested?
- **Sanitized templating**: Jinja/Helm template normalization

The Argo CD ApplicationSet, Crossplane, Kustomize, and Helm chart/values entries
that used to sit in this list were never untested — their coverage lived in the
parent parser package and is now in this package (issue #6062).

## Edge Cases Considered
- CloudFormation intrinsic tags (!Ref) in YAML templates
- Kubernetes resource with namespace/kind/name
- Source preservation under IndexSource option
- Observability extraction from multiple Helm values shapes (Grafana, Prometheus, OTel, Promtail, Loki, Tempo)
- Multi-document streams, anchors, aliases, merge keys, and recursive-alias rejection

## Edge Cases NOT Considered
- Empty YAML files
- Jinja-templated YAML (rendering not performed — only sanitized for struct decode)
- Deeply nested YAML structures
- Invalid YAML syntax

## Verdict
**deep** — 15 src files, with 14 test files cited here, including the Engine coverage relocated from the parent parser package in issue #6062. Covers Kubernetes resources, CloudFormation delegation, Pub dependencies, and comprehensive observability extraction across Grafana, Prometheus/Mimir, OTel pipelines, Promtail, Loki, and Tempo. As a permanent exception using `gopkg.in/yaml.v3` (canonical), this is extensive.

## Recommended Actions
- Document that YAML is a **permanent exception** — uses `gopkg.in/yaml.v3` with canonical YAML decoding, not tree-sitter
