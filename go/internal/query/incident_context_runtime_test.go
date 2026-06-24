// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildIncidentRuntimeEvidenceUsesExplicitPagerDutyOperationalLink(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(incidentRuntimeEvidenceInput{
		ServiceLink: incidentServiceCatalogOperationalLink{
			FactID:    "op-link",
			Provider:  "backstage",
			EntityRef: "component:default/checkout-api",
			URL:       "https://example.pagerduty.com/services/P-SVC",
		},
		CatalogCorrelations: []incidentServiceCatalogCorrelation{
			{
				FactID:       "catalog-correlation",
				EntityRef:    "component:default/checkout-api",
				DisplayName:  "Checkout API",
				RepositoryID: "repo-checkout",
				ServiceID:    "service:checkout-api",
				WorkloadID:   "workload:checkout-api",
				Outcome:      "exact",
				Reason:       "catalog repository id matches canonical repository identity",
			},
		},
		ImageIdentities: []incidentContainerImageIdentity{
			{
				FactID:           "image-identity",
				Digest:           "sha256:runtime",
				ImageRef:         "registry.example/checkout@sha256:runtime",
				RepositoryID:     "repo-checkout",
				Outcome:          "exact",
				IdentityStrength: "digest",
			},
		},
		KubernetesCorrelations: []incidentKubernetesCorrelation{
			{
				FactID:           "k8s-correlation",
				ClusterID:        "prod-cluster",
				Namespace:        "payments",
				WorkloadName:     "checkout-api",
				WorkloadObjectID: "k8s://prod/apps/v1/deployments/payments/checkout-api",
				ImageRef:         "registry.example/checkout@sha256:runtime",
				SourceDigest:     "sha256:runtime",
				Outcome:          "exact",
				Reason:           "live image digest matches an active deployment-source digest",
			},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotDeployable, IncidentTruthExact)
	assertIncidentEdge(t, got, IncidentSlotImage, IncidentTruthExact)
	assertIncidentEdge(t, got, IncidentSlotRuntimeArtifact, IncidentTruthExact)
}

func TestBuildIncidentRuntimeEvidenceAddsBuildAndCommitFromDigestCorrelation(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(baseIncidentRuntimeEvidenceInput(
		incidentContainerImageIdentity{
			FactID:           "image-identity",
			Digest:           "sha256:runtime",
			ImageRef:         "registry.example/checkout@sha256:runtime",
			RepositoryID:     "repo-checkout",
			Outcome:          "exact",
			IdentityStrength: "digest",
		},
		[]incidentCICDRunCorrelation{
			{
				FactID:          "run-correlation",
				Provider:        "github_actions",
				RunID:           "123",
				RepositoryID:    "repo-checkout",
				CommitSHA:       "abcdef1234567890",
				Environment:     "prod",
				ArtifactDigest:  "sha256:runtime",
				ImageRef:        "registry.example/checkout@sha256:runtime",
				Outcome:         "exact",
				CorrelationKind: "artifact_digest",
			},
		},
	))

	assertIncidentEdge(t, got, IncidentSlotBuildDeploy, IncidentTruthExact)
	assertIncidentEdge(t, got, IncidentSlotCommit, IncidentTruthExact)
}

func TestBuildIncidentRuntimeEvidenceTreatsTagOnlyCommitAsDerived(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(baseIncidentRuntimeEvidenceInput(
		incidentContainerImageIdentity{
			FactID:           "image-identity",
			ImageRef:         "registry.example/checkout:2026-06-01",
			RepositoryID:     "repo-checkout",
			Outcome:          "derived",
			IdentityStrength: "tag",
		},
		[]incidentCICDRunCorrelation{
			{
				FactID:       "run-correlation",
				Provider:     "github_actions",
				RunID:        "123",
				RepositoryID: "repo-checkout",
				CommitSHA:    "abcdef1234567890",
				Environment:  "prod",
				ImageRef:     "registry.example/checkout:2026-06-01",
				Outcome:      "exact",
			},
		},
	))

	assertIncidentEdge(t, got, IncidentSlotBuildDeploy, IncidentTruthDerived)
	assertIncidentEdge(t, got, IncidentSlotCommit, IncidentTruthDerived)
}

func TestBuildIncidentRuntimeEvidenceKeepsMultipleCommitCandidatesAmbiguous(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(baseIncidentRuntimeEvidenceInput(
		incidentContainerImageIdentity{
			FactID:       "image-identity",
			Digest:       "sha256:runtime",
			ImageRef:     "registry.example/checkout@sha256:runtime",
			RepositoryID: "repo-checkout",
			Outcome:      "exact",
		},
		[]incidentCICDRunCorrelation{
			{
				FactID:         "run-a",
				RunID:          "123",
				CommitSHA:      "commit-a",
				ArtifactDigest: "sha256:runtime",
				Outcome:        "exact",
			},
			{
				FactID:         "run-b",
				RunID:          "456",
				CommitSHA:      "commit-b",
				ArtifactDigest: "sha256:runtime",
				Outcome:        "exact",
			},
		},
	))

	build := incidentEdgeBySlot(t, got, IncidentSlotBuildDeploy)
	if build.TruthLabel != IncidentTruthAmbiguous {
		t.Fatalf("build truth_label = %q, want ambiguous", build.TruthLabel)
	}
	commit := incidentEdgeBySlot(t, got, IncidentSlotCommit)
	if commit.TruthLabel != IncidentTruthAmbiguous {
		t.Fatalf("commit truth_label = %q, want ambiguous", commit.TruthLabel)
	}
}

func TestBuildIncidentRuntimeEvidenceKeepsMultipleImagesAmbiguous(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(incidentRuntimeEvidenceInput{
		ServiceLink: incidentServiceCatalogOperationalLink{
			FactID:    "op-link",
			Provider:  "backstage",
			EntityRef: "component:default/checkout-api",
			URL:       "https://example.pagerduty.com/services/P-SVC",
		},
		CatalogCorrelations: []incidentServiceCatalogCorrelation{
			{
				FactID:       "catalog-correlation",
				EntityRef:    "component:default/checkout-api",
				RepositoryID: "repo-checkout",
				Outcome:      "exact",
			},
		},
		ImageIdentities: []incidentContainerImageIdentity{
			{
				FactID:           "image-a",
				Digest:           "sha256:a",
				ImageRef:         "registry.example/checkout@sha256:a",
				RepositoryID:     "repo-checkout",
				Outcome:          "exact",
				IdentityStrength: "digest",
			},
			{
				FactID:           "image-b",
				Digest:           "sha256:b",
				ImageRef:         "registry.example/checkout@sha256:b",
				RepositoryID:     "repo-checkout",
				Outcome:          "exact",
				IdentityStrength: "digest",
			},
		},
	})

	edge := incidentEdgeBySlot(t, got, IncidentSlotImage)
	if edge.TruthLabel != IncidentTruthAmbiguous {
		t.Fatalf("image truth_label = %q, want ambiguous", edge.TruthLabel)
	}
	if len(edge.Candidates) != 2 {
		t.Fatalf("image candidates = %d, want 2", len(edge.Candidates))
	}
	if runtime := findIncidentEdge(got, IncidentSlotRuntimeArtifact); runtime != nil {
		t.Fatalf("runtime artifact edge = %#v, want nil without a single image", runtime)
	}
}

func TestBuildIncidentRuntimeEvidenceDoesNotUseImagesWithoutSingleDeployable(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(incidentRuntimeEvidenceInput{
		ServiceLink: incidentServiceCatalogOperationalLink{
			FactID:    "op-link",
			Provider:  "backstage",
			EntityRef: "component:default/checkout-api",
			URL:       "https://example.pagerduty.com/services/P-SVC",
		},
		CatalogCorrelations: []incidentServiceCatalogCorrelation{
			{
				FactID:                 "catalog-correlation",
				EntityRef:              "component:default/checkout-api",
				Outcome:                "ambiguous",
				CandidateRepositoryIDs: []string{"repo-a", "repo-b"},
			},
		},
		ImageIdentities: []incidentContainerImageIdentity{
			{
				FactID:       "image-a",
				Digest:       "sha256:a",
				ImageRef:     "registry.example/checkout@sha256:a",
				RepositoryID: "repo-a",
				Outcome:      "exact",
			},
		},
	})

	assertIncidentEdge(t, got, IncidentSlotDeployable, IncidentTruthAmbiguous)
	if image := findIncidentEdge(got, IncidentSlotImage); image != nil {
		t.Fatalf("image edge = %#v, want nil without one deployable repository", image)
	}
}

func TestBuildIncidentRuntimeEvidenceRequiresExplicitServiceLink(t *testing.T) {
	t.Parallel()

	got := buildIncidentRuntimeEvidence(incidentRuntimeEvidenceInput{
		CatalogCorrelations: []incidentServiceCatalogCorrelation{
			{
				FactID:       "catalog-correlation",
				DisplayName:  "Checkout API",
				RepositoryID: "repo-checkout",
				Outcome:      "exact",
			},
		},
		ImageIdentities: []incidentContainerImageIdentity{
			{
				FactID:       "image-identity",
				Digest:       "sha256:runtime",
				ImageRef:     "registry.example/checkout@sha256:runtime",
				RepositoryID: "repo-checkout",
				Outcome:      "exact",
			},
		},
	})

	if len(got) != 0 {
		t.Fatalf("runtime evidence = %#v, want none without explicit PagerDuty operational link", got)
	}
}

func incidentEdgeBySlot(
	t *testing.T,
	edges []IncidentContextEvidenceEdge,
	slot IncidentEvidenceSlot,
) IncidentContextEvidenceEdge {
	t.Helper()
	edge := findIncidentEdge(edges, slot)
	if edge == nil {
		t.Fatalf("missing edge %s in %#v", slot, edges)
	}
	return *edge
}

func findIncidentEdge(
	edges []IncidentContextEvidenceEdge,
	slot IncidentEvidenceSlot,
) *IncidentContextEvidenceEdge {
	for idx := range edges {
		if edges[idx].Slot == slot {
			return &edges[idx]
		}
	}
	return nil
}

func baseIncidentRuntimeEvidenceInput(
	image incidentContainerImageIdentity,
	runs []incidentCICDRunCorrelation,
) incidentRuntimeEvidenceInput {
	return incidentRuntimeEvidenceInput{
		ServiceLink: incidentServiceCatalogOperationalLink{
			FactID:    "op-link",
			Provider:  "backstage",
			EntityRef: "component:default/checkout-api",
			URL:       "https://example.pagerduty.com/services/P-SVC",
		},
		CatalogCorrelations: []incidentServiceCatalogCorrelation{
			{
				FactID:       "catalog-correlation",
				EntityRef:    "component:default/checkout-api",
				DisplayName:  "Checkout API",
				RepositoryID: "repo-checkout",
				ServiceID:    "service:checkout-api",
				WorkloadID:   "workload:checkout-api",
				Outcome:      "exact",
			},
		},
		ImageIdentities:     []incidentContainerImageIdentity{image},
		CICDRunCorrelations: runs,
	}
}
