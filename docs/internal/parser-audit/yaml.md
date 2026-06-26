# YAML Parser Audit

## Overview
Parses YAML-family configuration files using `gopkg.in/yaml.v3`. This is a **declarative data** parser — NOT a language parser. Extracts Kubernetes resources, Argo CD applications/ApplicationSets, Crossplane resources (XRDs, compositions, claims), Kustomize overlays, Helm chart/values metadata, CloudFormation/SAM templates (delegated to cloudformation package), Pub dependencies, and metadata-only observability rows from Helm values, Grafana, Prometheus/Mimir, Loki, Tempo, OTel pipelines, Promtail, and Argo CD status resources. 15 src files, 7 test files. No regexp.MustCompile.

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
- `TestParseKubernetesResourceDirectly` (`language_test.go:12`): k8s_resources with qualified_name, container_images, source preservation
- `TestParseCloudFormationIntrinsicDirectly` (`language_test.go:54`): CloudFormation resource, parameter, and output extraction from YAML
- `observability_test.go`: Helm Grafana observability from values.yaml
- `observability_metrics_test.go`: OTel metric pipeline and Prometheus scrape configs
- `observability_log_routes_test.go`: Promtail client routes and OTel log pipelines
- `observability_trace_routes_test.go`: OTel trace pipelines and Tempo gateway values
- `observability_applied_test.go`: Argo CD status resources and API-exported observability
- `pubspec_test.go`: Pub dependency extraction from pubspec.yaml/pubspec.lock
- Parent-level: 9 parent test files reference yaml parsing

## Unverified / Claimed-but-Untested Constructs
- **Argo CD ApplicationSets**: claimed in doc.go but no explicit ApplicationSet test file in yaml package tests
- **Crossplane XRDs/compositions/claims**: likely covered in parent-level tests or `semantics_test.go` equivalent; no dedicated crossplane test file in yaml package
- **Kustomize overlays**: no dedicated kustomize test file
- **Helm Chart.yaml**: no dedicated helm test file visible
- **Helm values.yaml non-Grafana observability**: Prometheus Operator scrape/rule resources, Loki gateway values not explicitly listed in test file names
- **Helm template manifests**: skip behavior tested?
- **Sanitized templating**: Jinja/Helm template normalization

## Edge Cases Considered
- CloudFormation intrinsic tags (!Ref) in YAML templates
- Kubernetes resource with namespace/kind/name
- Source preservation under IndexSource option
- Observability extraction from multiple Helm values shapes (Grafana, Prometheus, OTel, Promtail, Loki, Tempo)

## Edge Cases NOT Considered
- Multi-document YAML files (--- separator)
- Empty YAML files
- YAML with anchors and aliases
- YAML with merge keys (<<)
- Jinja-templated YAML (rendering not performed — only sanitized for struct decode)
- Deeply nested YAML structures
- Invalid YAML syntax

## Verdict
**deep** — 15 src files with 7 internal test files plus 9 parent-level test files. Covers Kubernetes resources, CloudFormation delegation, Pub dependencies, and comprehensive observability extraction across Grafana, Prometheus/Mimir, OTel pipelines, Promtail, Loki, and Tempo. As a permanent exception using `gopkg.in/yaml.v3` (canonical), this is extensive.

## Recommended Actions
- Document that YAML is a **permanent exception** — uses `gopkg.in/yaml.v3` with canonical YAML decoding, not tree-sitter
- Verify Argo CD ApplicationSet tests exist (may be in language_test.go beyond what was read or in parent-level tests)
- Verify Crossplane/Kustomize test coverage location (parent-level tests or internal tests)
- Add a test for multi-document YAML files (if supported)
- Add a test for YAML with anchors and aliases
