// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSBOMAttestationAttachmentDecisionsCarriesImageAnchors(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		containerImageIdentityAnchorFact(
			"image-identity",
			testSBOMSubjectDigest,
			[]string{"repo://example/api"},
			[]string{"workload:example-api"},
			[]string{"service:example-api"},
			string(ContainerImageIdentityTagResolved),
		),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)["doc-verified"]
	if !slices.Contains(got.RepositoryIDs, "repo://example/api") {
		t.Fatalf("RepositoryIDs = %#v, want repo://example/api", got.RepositoryIDs)
	}
	if !slices.Contains(got.WorkloadIDs, "workload:example-api") {
		t.Fatalf("WorkloadIDs = %#v, want workload:example-api", got.WorkloadIDs)
	}
	if !slices.Contains(got.ServiceIDs, "service:example-api") {
		t.Fatalf("ServiceIDs = %#v, want service:example-api", got.ServiceIDs)
	}
	if len(got.MissingEvidence) != 0 {
		t.Fatalf("MissingEvidence = %#v, want empty with image anchor", got.MissingEvidence)
	}
}

func TestBuildSBOMAttestationAttachmentDecisionsKeepsMissingImageHopExplicit(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		containerImageIdentityAnchorFact(
			"image-ambiguous",
			testSBOMSubjectDigest,
			nil,
			nil,
			nil,
			string(ContainerImageIdentityAmbiguousTag),
		),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)["doc-verified"]
	if !slices.Contains(got.MissingEvidence, "repository_to_image_evidence_missing") {
		t.Fatalf("MissingEvidence = %#v, want repository_to_image_evidence_missing", got.MissingEvidence)
	}
}

func containerImageIdentityAnchorFact(
	factID string,
	digest string,
	repositoryIDs []string,
	workloadIDs []string,
	serviceIDs []string,
	outcome string,
) facts.Envelope {
	values := map[string]any{
		"digest":            digest,
		"image_ref":         "registry.example.com/team/api:prod",
		"repository_id":     "oci-registry://registry.example.com/team/api",
		"outcome":           outcome,
		"canonical_writes":  1,
		"source_layers":     []any{"source_declaration", "observed_resource"},
		"evidence_fact_ids": []any{"git-tag", "oci-tag"},
	}
	if len(repositoryIDs) > 0 {
		values["source_repository_ids"] = stringsToAny(repositoryIDs)
	}
	if len(workloadIDs) > 0 {
		values["workload_ids"] = stringsToAny(workloadIDs)
	}
	if len(serviceIDs) > 0 {
		values["service_ids"] = stringsToAny(serviceIDs)
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: containerImageIdentityFactKind,
		Payload:  values,
	}
}

func stringsToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
