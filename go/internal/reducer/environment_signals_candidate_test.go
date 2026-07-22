// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractWorkloadCandidatesArgoCDDestinationNamespaceFeedsEnvironments is
// the positive case for issue #5444: an ArgoCD Application's
// destination.namespace ("production") was captured by the parser
// (dest_namespace) but never read for deploymentEnvironments -- workloads
// with this explicit environment evidence "one struct away" got nothing.
// Alias-gated through environment.Canonical, "production" resolves to "prod".
func TestExtractWorkloadCandidatesArgoCDDestinationNamespaceFeedsEnvironments(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-argocd-ns",
				"name":     "argocd-ns-app",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-argocd-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-argocd-ns",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{
						map[string]any{
							"name":           "argocd-ns-app",
							"source_repo":    "https://github.com/org/argocd-ns-app",
							"source_path":    "deploy",
							"dest_namespace": "production",
						},
					},
				},
			},
			ObservedAt: now,
		},
	}

	_, deploymentEnvs := ExtractWorkloadCandidates(envelopes)
	envs := deploymentEnvs["repo-argocd-ns"]
	if len(envs) != 1 || envs[0] != "prod" {
		t.Fatalf("deployment environments for repo-argocd-ns = %v, want [prod]", envs)
	}
}

// TestExtractWorkloadCandidatesArgoCDDestinationNamespaceUnknownIsNegative is
// the negative case: a destination.namespace that is not a known
// environment token ("payments-team") must never invent an environment.
func TestExtractWorkloadCandidatesArgoCDDestinationNamespaceUnknownIsNegative(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-argocd-ns-neg",
				"name":     "argocd-ns-neg-app",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-argocd-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-argocd-ns-neg",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{
						map[string]any{
							"name":           "argocd-ns-neg-app",
							"source_repo":    "https://github.com/org/argocd-ns-neg-app",
							"source_path":    "deploy",
							"dest_namespace": "payments-team",
						},
					},
				},
			},
			ObservedAt: now,
		},
	}

	_, deploymentEnvs := ExtractWorkloadCandidates(envelopes)
	if envs := deploymentEnvs["repo-argocd-ns-neg"]; len(envs) != 0 {
		t.Fatalf("deployment environments for repo-argocd-ns-neg = %v, want none (no invented environment)", envs)
	}
}

// TestExtractWorkloadCandidatesKustomizeNamespaceFeedsEnvironments proves the
// Kustomize destination-namespace source: a kustomization.yaml's namespace
// field ("staging") feeds deploymentEnvironments, alias-gated to "stage".
func TestExtractWorkloadCandidatesKustomizeNamespaceFeedsEnvironments(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-kustomize-ns",
				"name":     "kustomize-ns-app",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-kustomization",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-kustomize-ns",
				"parsed_file_data": map[string]any{
					"k8s_resources": []any{
						map[string]any{"name": "app", "kind": "Deployment", "namespace": "staging"},
					},
					"kustomize_overlays": []any{
						map[string]any{"name": "kustomization", "namespace": "staging"},
					},
				},
				"relative_path": "overlays/staging/kustomization.yaml",
			},
			ObservedAt: now,
		},
	}

	_, deploymentEnvs := ExtractWorkloadCandidates(envelopes)
	envs := deploymentEnvs["repo-kustomize-ns"]
	if len(envs) != 1 || envs[0] != "stage" {
		t.Fatalf("deployment environments for repo-kustomize-ns = %v, want [stage]", envs)
	}
}

// TestExtractWorkloadCandidatesHelmValuesFilenameFeedsEnvironments proves the
// Helm values-<env>.yaml filename convention feeds deploymentEnvironments
// even without a matching overlays//env/environments directory ancestor.
func TestExtractWorkloadCandidatesHelmValuesFilenameFeedsEnvironments(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-helm-values",
				"name":     "helm-values-app",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-chart",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-helm-values",
				"parsed_file_data": map[string]any{
					"chart_type": "application",
				},
				"relative_path": "charts/app/Chart.yaml",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-values-prod",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-helm-values",
				"parsed_file_data": map[string]any{
					"helm_values": []any{
						map[string]any{"name": "values-prod"},
					},
				},
				"relative_path": "charts/app/values-prod.yaml",
			},
			ObservedAt: now,
		},
	}

	_, deploymentEnvs := ExtractWorkloadCandidates(envelopes)
	envs := deploymentEnvs["repo-helm-values"]
	if len(envs) != 1 || envs[0] != "prod" {
		t.Fatalf("deployment environments for repo-helm-values = %v, want [prod]", envs)
	}
}

// TestExtractWorkloadCandidatesHelmValuesSchemaFileIsAmbiguousNegative proves
// the ambiguous case: values.schema.yaml looks like a values-<env>.yaml
// override but "schema" is not a known environment token, so no environment
// is invented.
func TestExtractWorkloadCandidatesHelmValuesSchemaFileIsAmbiguousNegative(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-helm-schema",
				"name":     "helm-schema-app",
			},
			ObservedAt: now,
		},
		{
			FactID:   "fact-file-values-schema",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo-helm-schema",
				"relative_path": "charts/app/values.schema.yaml",
			},
			ObservedAt: now,
		},
	}

	_, deploymentEnvs := ExtractWorkloadCandidates(envelopes)
	if envs := deploymentEnvs["repo-helm-schema"]; len(envs) != 0 {
		t.Fatalf("deployment environments for repo-helm-schema = %v, want none (ambiguous, not admitted)", envs)
	}
}

// TestExtractOverlayEnvironmentsFromEnvelopesReadsArgoCDDestinationNamespace
// proves the cross-repo deployment-source enrichment path
// (enrichDeploymentRepoEnvironments -> ExtractOverlayEnvironmentsFromEnvelopes)
// picks up the same broadened namespace signal for a config-only deploy
// repo, not just the candidate-extraction path.
func TestExtractOverlayEnvironmentsFromEnvelopesReadsArgoCDDestinationNamespace(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	envelopes := []facts.Envelope{
		{
			FactID:   "fact-file-argocd-app",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-deploy-manifests",
				"parsed_file_data": map[string]any{
					"argocd_applications": []any{
						map[string]any{
							"name":           "svc",
							"dest_namespace": "prod",
						},
					},
				},
			},
			ObservedAt: now,
		},
	}

	got := ExtractOverlayEnvironmentsFromEnvelopes(envelopes)
	envs := got["repo-deploy-manifests"]
	if len(envs) != 1 || envs[0] != "prod" {
		t.Fatalf("deployment environments for repo-deploy-manifests = %v, want [prod]", envs)
	}
}
