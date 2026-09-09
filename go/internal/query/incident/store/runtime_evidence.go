// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type incidentRuntimeEvidenceInput struct {
	ServiceLink            incidentServiceCatalogOperationalLink
	CatalogCorrelations    []incidentServiceCatalogCorrelation
	ImageIdentities        []incidentContainerImageIdentity
	KubernetesCorrelations []incidentKubernetesCorrelation
	CICDRunCorrelations    []incidentCICDRunCorrelation
}

type incidentServiceCatalogOperationalLink struct {
	FactID    string
	Provider  string
	EntityRef string
	LinkType  string
	Title     string
	URL       string
}

type incidentServiceCatalogCorrelation struct {
	FactID                 string
	Provider               string
	EntityRef              string
	DisplayName            string
	RepositoryID           string
	ServiceID              string
	WorkloadID             string
	OwnerRef               string
	Outcome                string
	Reason                 string
	ProvenanceOnly         bool
	CandidateRepositoryIDs []string
	EvidenceFactIDs        []string
}

type incidentContainerImageIdentity struct {
	FactID           string
	Digest           string
	ImageRef         string
	RepositoryID     string
	Outcome          string
	Reason           string
	IdentityStrength string
	CanonicalID      string
	EvidenceFactIDs  []string
}

type incidentKubernetesCorrelation struct {
	FactID                 string
	ClusterID              string
	WorkloadObjectID       string
	Namespace              string
	WorkloadName           string
	ImageRef               string
	SourceDigest           string
	JoinMode               string
	Outcome                string
	Reason                 string
	ProvenanceOnly         bool
	CandidateSourceDigests []string
	EvidenceFactIDs        []string
}

func buildIncidentRuntimeEvidence(
	input incidentRuntimeEvidenceInput,
) []model.IncidentContextEvidenceEdge {
	if strings.TrimSpace(input.ServiceLink.EntityRef) == "" {
		return nil
	}

	deployableEdge, selectedCatalog := buildIncidentDeployableEdge(
		input.ServiceLink,
		input.CatalogCorrelations,
	)
	if deployableEdge == nil {
		return nil
	}

	edges := []model.IncidentContextEvidenceEdge{*deployableEdge}
	if selectedCatalog.RepositoryID == "" {
		return edges
	}
	imageEdge, selectedImage := buildIncidentImageEdge(
		input.ImageIdentities,
		selectedCatalog.RepositoryID,
	)
	if imageEdge != nil {
		edges = append(edges, *imageEdge)
	}
	if selectedImage != nil {
		edges = append(edges, buildIncidentBuildCommitEdges(
			input.CICDRunCorrelations,
			*selectedImage,
		)...)
		if runtimeEdge := buildIncidentRuntimeArtifactEdge(
			input.KubernetesCorrelations,
			*selectedImage,
		); runtimeEdge != nil {
			edges = append(edges, *runtimeEdge)
		}
	}
	return edges
}

func buildIncidentDeployableEdge(
	link incidentServiceCatalogOperationalLink,
	correlations []incidentServiceCatalogCorrelation,
) (*model.IncidentContextEvidenceEdge, incidentServiceCatalogCorrelation) {
	candidates := incidentCatalogPromotionCandidates(correlations)
	if len(candidates) == 0 {
		return nil, incidentServiceCatalogCorrelation{}
	}
	if len(candidates) > 1 {
		return &model.IncidentContextEvidenceEdge{
			Slot:        model.IncidentSlotDeployable,
			TruthLabel:  model.IncidentTruthAmbiguous,
			Explanation: "PagerDuty service URL matched a service-catalog entity, but multiple catalog correlations remain possible",
			Evidence:    incidentEvidenceRefsForServiceLink(link),
			Candidates:  incidentCatalogCandidates(candidates),
		}, incidentServiceCatalogCorrelation{}
	}

	correlation := candidates[0]
	label := incidentTruthFromReducerOutcome(correlation.Outcome)
	edge := model.IncidentContextEvidenceEdge{
		Slot:        model.IncidentSlotDeployable,
		TruthLabel:  label,
		Explanation: incidentDeployableExplanation(correlation),
		Value: map[string]string{
			"entity_ref":    correlation.EntityRef,
			"display_name":  correlation.DisplayName,
			"repository_id": correlation.RepositoryID,
			"service_id":    correlation.ServiceID,
			"workload_id":   correlation.WorkloadID,
			"owner_ref":     correlation.OwnerRef,
		},
		Evidence: append(
			incidentEvidenceRefsForServiceLink(link),
			incidentEvidenceRef("reducer_service_catalog_correlation", correlation.FactID, "", ""),
		),
	}
	return &edge, correlation
}

func incidentCatalogPromotionCandidates(
	correlations []incidentServiceCatalogCorrelation,
) []incidentServiceCatalogCorrelation {
	out := make([]incidentServiceCatalogCorrelation, 0, len(correlations))
	for _, correlation := range correlations {
		switch strings.TrimSpace(correlation.Outcome) {
		case string(model.IncidentTruthExact), string(model.IncidentTruthDerived):
			if correlation.RepositoryID != "" || correlation.ServiceID != "" || correlation.WorkloadID != "" {
				out = append(out, correlation)
			}
		case string(model.IncidentTruthAmbiguous):
			out = append(out, correlation)
		}
	}
	return out
}

func incidentCatalogCandidates(
	correlations []incidentServiceCatalogCorrelation,
) []model.IncidentContextEvidenceCandidate {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(correlations))
	for _, correlation := range correlations {
		if len(correlation.CandidateRepositoryIDs) > 0 {
			for _, repositoryID := range correlation.CandidateRepositoryIDs {
				candidates = append(candidates, model.IncidentContextEvidenceCandidate{
					ID:     repositoryID,
					Label:  correlation.DisplayName,
					Reason: querycontract.FirstNonEmpty(correlation.Reason, "catalog correlation candidate"),
				})
			}
			continue
		}
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     querycontract.FirstNonEmpty(correlation.WorkloadID, correlation.ServiceID, correlation.RepositoryID),
			Label:  correlation.DisplayName,
			Reason: querycontract.FirstNonEmpty(correlation.Reason, "catalog correlation candidate"),
		})
	}
	return candidates
}

func buildIncidentImageEdge(
	images []incidentContainerImageIdentity,
	repositoryID string,
) (*model.IncidentContextEvidenceEdge, *incidentContainerImageIdentity) {
	candidates := incidentImagePromotionCandidates(images, repositoryID)
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 1 {
		return &model.IncidentContextEvidenceEdge{
			Slot:        model.IncidentSlotImage,
			TruthLabel:  model.IncidentTruthAmbiguous,
			Explanation: "multiple active container image identities match the incident deployable; no single image was selected",
			Candidates:  incidentImageCandidates(candidates),
		}, nil
	}
	image := candidates[0]
	edge := model.IncidentContextEvidenceEdge{
		Slot:        model.IncidentSlotImage,
		TruthLabel:  incidentTruthFromReducerOutcome(image.Outcome),
		Explanation: incidentImageExplanation(image),
		Value: map[string]string{
			"digest":            image.Digest,
			"image_ref":         image.ImageRef,
			"repository_id":     image.RepositoryID,
			"identity_strength": image.IdentityStrength,
			"canonical_id":      image.CanonicalID,
		},
		Evidence: []model.IncidentContextEvidenceRef{
			incidentEvidenceRef("reducer_container_image_identity", image.FactID, "", ""),
		},
	}
	return &edge, &image
}

func incidentImagePromotionCandidates(
	images []incidentContainerImageIdentity,
	repositoryID string,
) []incidentContainerImageIdentity {
	exact := make([]incidentContainerImageIdentity, 0, len(images))
	derived := make([]incidentContainerImageIdentity, 0, len(images))
	for _, image := range images {
		if repositoryID != "" && image.RepositoryID != "" && image.RepositoryID != repositoryID {
			continue
		}
		switch strings.TrimSpace(image.Outcome) {
		case string(model.IncidentTruthExact):
			if image.Digest != "" {
				exact = append(exact, image)
			}
		case string(model.IncidentTruthDerived):
			derived = append(derived, image)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return derived
}

func incidentImageCandidates(
	images []incidentContainerImageIdentity,
) []model.IncidentContextEvidenceCandidate {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(images))
	for _, image := range images {
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     querycontract.FirstNonEmpty(image.Digest, image.ImageRef, image.FactID),
			Label:  image.ImageRef,
			Reason: querycontract.FirstNonEmpty(image.Reason, "container image identity candidate"),
		})
	}
	return candidates
}

func buildIncidentRuntimeArtifactEdge(
	correlations []incidentKubernetesCorrelation,
	image incidentContainerImageIdentity,
) *model.IncidentContextEvidenceEdge {
	candidates := incidentKubernetesPromotionCandidates(correlations, image)
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) > 1 {
		return &model.IncidentContextEvidenceEdge{
			Slot:        model.IncidentSlotRuntimeArtifact,
			TruthLabel:  model.IncidentTruthAmbiguous,
			Explanation: "multiple live Kubernetes workload correlations match the incident image; no single runtime artifact was selected",
			Candidates:  incidentKubernetesCandidates(candidates),
		}
	}
	correlation := candidates[0]
	return &model.IncidentContextEvidenceEdge{
		Slot:        model.IncidentSlotRuntimeArtifact,
		TruthLabel:  incidentTruthFromReducerOutcome(correlation.Outcome),
		Explanation: incidentRuntimeArtifactExplanation(correlation),
		Value: map[string]string{
			"cluster_id":         correlation.ClusterID,
			"namespace":          correlation.Namespace,
			"workload_name":      correlation.WorkloadName,
			"workload_object_id": correlation.WorkloadObjectID,
			"image_ref":          correlation.ImageRef,
			"source_digest":      correlation.SourceDigest,
			"join_mode":          correlation.JoinMode,
		},
		Evidence: []model.IncidentContextEvidenceRef{
			incidentEvidenceRef("reducer_kubernetes_correlation", correlation.FactID, "", ""),
		},
	}
}

func incidentKubernetesPromotionCandidates(
	correlations []incidentKubernetesCorrelation,
	image incidentContainerImageIdentity,
) []incidentKubernetesCorrelation {
	out := make([]incidentKubernetesCorrelation, 0, len(correlations))
	for _, correlation := range correlations {
		if !incidentKubernetesMatchesImage(correlation, image) {
			continue
		}
		switch strings.TrimSpace(correlation.Outcome) {
		case string(model.IncidentTruthExact), string(model.IncidentTruthDerived):
			out = append(out, correlation)
		case string(model.IncidentTruthAmbiguous):
			out = append(out, correlation)
		}
	}
	return out
}

func incidentKubernetesMatchesImage(
	correlation incidentKubernetesCorrelation,
	image incidentContainerImageIdentity,
) bool {
	if image.Digest != "" && correlation.SourceDigest == image.Digest {
		return true
	}
	return image.ImageRef != "" && correlation.ImageRef == image.ImageRef
}

func incidentKubernetesCandidates(
	correlations []incidentKubernetesCorrelation,
) []model.IncidentContextEvidenceCandidate {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(correlations))
	for _, correlation := range correlations {
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     querycontract.FirstNonEmpty(correlation.WorkloadObjectID, correlation.SourceDigest, correlation.ImageRef),
			Label:  querycontract.FirstNonEmpty(correlation.WorkloadName, correlation.ImageRef),
			Reason: querycontract.FirstNonEmpty(correlation.Reason, "Kubernetes correlation candidate"),
		})
	}
	return candidates
}

func incidentTruthFromReducerOutcome(outcome string) model.IncidentTruthLabel {
	switch strings.TrimSpace(outcome) {
	case string(model.IncidentTruthExact):
		return model.IncidentTruthExact
	case string(model.IncidentTruthDerived):
		return model.IncidentTruthDerived
	case string(model.IncidentTruthAmbiguous):
		return model.IncidentTruthAmbiguous
	case "unresolved", "stale", "rejected":
		return model.IncidentTruthMissing
	default:
		return model.IncidentTruthMissing
	}
}

func incidentEvidenceRefsForServiceLink(
	link incidentServiceCatalogOperationalLink,
) []model.IncidentContextEvidenceRef {
	return []model.IncidentContextEvidenceRef{
		incidentEvidenceRef("service_catalog.operational_link", link.FactID, link.URL, link.Provider),
	}
}

func incidentEvidenceRef(
	kind string,
	factID string,
	url string,
	source string,
) model.IncidentContextEvidenceRef {
	return model.IncidentContextEvidenceRef{
		FactID: factID,
		Kind:   kind,
		URL:    url,
		Source: source,
	}
}

func incidentDeployableExplanation(correlation incidentServiceCatalogCorrelation) string {
	if correlation.Reason != "" {
		return "PagerDuty service URL matched service-catalog operational link; " + correlation.Reason
	}
	return "PagerDuty service URL matched service-catalog operational link and catalog correlation"
}

func incidentImageExplanation(image incidentContainerImageIdentity) string {
	if image.Reason != "" {
		return image.Reason
	}
	if image.Digest != "" {
		return "container image identity resolved an immutable digest for the incident deployable"
	}
	return "container image identity resolved an image reference for the incident deployable"
}

func incidentRuntimeArtifactExplanation(correlation incidentKubernetesCorrelation) string {
	if correlation.Reason != "" {
		return correlation.Reason
	}
	return "live Kubernetes correlation matched the incident image evidence"
}
