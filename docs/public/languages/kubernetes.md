# Kubernetes Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `kubernetes`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/kubernetes_comprehensive/`
- Unit test suite: `go/internal/parser/engine_infra_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Kubernetes resources (any `apiVersion`/`kind`) | `kubernetes-resources-any-apiversion-kind` | supported | `k8s_resources` | `name, line_number, kind, api_version` | `node:K8sResource` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | - |
| API version | `api-version` | supported | `k8s_resources` | `name, line_number, api_version` | `property:K8sResource.api_version` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | - |
| Kind | `kind` | supported | `k8s_resources` | `name, line_number, kind` | `property:K8sResource.kind` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | - |
| Name (`metadata.name`) | `name-metadata-name` | supported | `k8s_resources` | `name, line_number` | `property:K8sResource.name` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | - |
| Namespace (`metadata.namespace`) | `namespace-metadata-namespace` | supported | `k8s_resources` | `name, line_number, namespace` | `property:K8sResource.namespace` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | - |
| Labels | `labels` | supported | `k8s_resources` | `name, line_number, labels` | `property:K8sResource.labels` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | `metadata.labels` is normalized into a stable `key=value` string on the Kubernetes payload. |
| Qualified resource identity | `qualified-name` | supported | `k8s_resources` | `name, line_number, qualified_name` | `property:K8sResource.qualified_name` | `go/internal/parser/engine_kubernetes_semantics_test.go::TestDefaultEngineParsePathYAMLKubernetesQualifiedName` | Compose-backed fixture verification | `metadata.namespace`, `kind`, and `metadata.name` are normalized into a stable `namespace/kind/name` identity string. |
| Service-to-Deployment selector matching | `service-selects-deployment` | supported | content-backed relationships | `name, namespace, kind, selector, pod_template_labels` | `relationship:SELECTS` | `go/internal/query/content_relationships_k8s_match_test.go`, `go/internal/query/content_relationships_k8s_test.go::TestBuildContentRelationshipSetK8sServiceSelectsDifferentlyNamedDeploymentBySelector`, `go/internal/query/content_relationships_k8s_test.go::TestBuildContentRelationshipSetK8sAnchorSelectorMismatchNeverFallsBackToName`, `go/internal/query/impact_trace_deployment_k8s_test.go`, `go/internal/query/impact_trace_deployment_k8s_select_widening_test.go` | `go/internal/query/entity_content_iac_fallback_test.go::TestGetEntityContextFallsBackToKubernetesResourceContentEntity` | Matches a Service's `spec.selector` against the target workload's (Deployment, v1 scope) `spec.template.metadata.labels`, strictly namespace-scoped. Falls back to the same-name, same-namespace heuristic only when the selector is unknown (pre-upgrade content row); a known selector is authoritative and never falls back, even on a name/namespace coincidence. On the impact-trace deployment-chain surface a differently-named Service that selector-matches the traced Deployment is discovered via a directed candidate scan and joined to `k8s_resources` (#5363); truncation of that scan is disclosed as `k8s_relationships_complete=false`. |
| Multi-document YAML support | `multi-document-yaml-support` | supported | `multi-document-yaml-support` | `name, line_number` | `node:multi-document-yaml-support` | `go/internal/parser/engine_infra_test.go::TestDefaultEngineParsePathYAMLKubernetes` | Compose-backed fixture verification | - |

## Framework And Library Support

Supported today:

- Kubernetes is deployment/runtime configuration evidence, not
  application-framework reachability.
- Kubernetes resources, identity fields, labels, Service-to-Deployment
  selector matching (with a same-name/namespace fallback for pre-upgrade
  content), and multi-document YAML are modeled.

Not claimed today:

- Pod image extraction, CRD schema semantics, and controller runtime behavior
  are not modeled as framework support. Selector matching is scoped to
  Deployment workloads in v1; StatefulSet, DaemonSet, and ReplicaSet
  pod-template labels are captured by the parser but not yet matched.

## Known Limitations
- Container image references within Pod specs are not extracted as separate nodes
- Service selector matching is Deployment-only in v1; StatefulSet, DaemonSet, and ReplicaSet are not yet matched even though their pod-template labels are captured
- Custom Resource Definitions (CRDs) are parsed as generic K8s resources without schema awareness
