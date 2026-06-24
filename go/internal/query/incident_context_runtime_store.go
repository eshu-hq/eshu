// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

const incidentRuntimeEvidenceLimit = 25

func (s PostgresIncidentContextStore) readIncidentRuntimeEvidence(
	ctx context.Context,
	incident IncidentContextIncident,
) ([]IncidentContextEvidenceEdge, error) {
	serviceURL := strings.TrimSpace(incident.Service.URL)
	if serviceURL == "" {
		return nil, nil
	}

	links, err := s.readIncidentServiceCatalogLinks(ctx, serviceURL)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, nil
	}
	if len(links) > 1 {
		return []IncidentContextEvidenceEdge{ambiguousIncidentServiceLinkEdge(links)}, nil
	}

	link := links[0]
	correlations, err := s.readIncidentServiceCatalogCorrelations(ctx, link)
	if err != nil {
		return nil, err
	}
	input := incidentRuntimeEvidenceInput{
		ServiceLink:         link,
		CatalogCorrelations: correlations,
	}
	catalogCandidates := incidentCatalogPromotionCandidates(correlations)
	if len(catalogCandidates) != 1 || catalogCandidates[0].RepositoryID == "" {
		return buildIncidentRuntimeEvidence(input), nil
	}

	images, err := s.readIncidentContainerImageIdentities(ctx, catalogCandidates[0].RepositoryID)
	if err != nil {
		return nil, err
	}
	input.ImageIdentities = images
	imageCandidates := incidentImagePromotionCandidates(images, catalogCandidates[0].RepositoryID)
	if len(imageCandidates) == 1 {
		cicd, err := s.readIncidentCICDRunCorrelations(ctx, imageCandidates[0])
		if err != nil {
			return nil, err
		}
		input.CICDRunCorrelations = cicd
		kubernetes, err := s.readIncidentKubernetesCorrelations(ctx, imageCandidates[0])
		if err != nil {
			return nil, err
		}
		input.KubernetesCorrelations = kubernetes
	}
	return buildIncidentRuntimeEvidence(input), nil
}

func (s PostgresIncidentContextStore) readIncidentServiceCatalogLinks(
	ctx context.Context,
	serviceURL string,
) ([]incidentServiceCatalogOperationalLink, error) {
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentServiceCatalogOperationalLinksQuery,
		serviceURL,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident service catalog links: %w", err)
	}
	links := make([]incidentServiceCatalogOperationalLink, 0, len(rows))
	for _, row := range rows {
		links = append(links, decodeIncidentServiceCatalogOperationalLink(row))
	}
	return links, nil
}

func (s PostgresIncidentContextStore) readIncidentServiceCatalogCorrelations(
	ctx context.Context,
	link incidentServiceCatalogOperationalLink,
) ([]incidentServiceCatalogCorrelation, error) {
	store := NewPostgresServiceCatalogCorrelationStore(s.DB)
	rows, err := store.ListServiceCatalogCorrelations(ctx, ServiceCatalogCorrelationFilter{
		Provider:  link.Provider,
		EntityRef: link.EntityRef,
		Limit:     incidentRuntimeEvidenceLimit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list incident service catalog correlations: %w", err)
	}
	correlations := make([]incidentServiceCatalogCorrelation, 0, len(rows))
	for _, row := range rows {
		correlations = append(correlations, incidentServiceCatalogCorrelation{
			FactID:                 row.CorrelationID,
			Provider:               row.Provider,
			EntityRef:              row.EntityRef,
			DisplayName:            row.DisplayName,
			RepositoryID:           row.RepositoryID,
			ServiceID:              row.ServiceID,
			WorkloadID:             row.WorkloadID,
			OwnerRef:               row.OwnerRef,
			Outcome:                row.Outcome,
			Reason:                 row.Reason,
			ProvenanceOnly:         row.ProvenanceOnly,
			CandidateRepositoryIDs: nil,
			EvidenceFactIDs:        row.EvidenceFactIDs,
		})
	}
	return correlations, nil
}

func (s PostgresIncidentContextStore) readIncidentContainerImageIdentities(
	ctx context.Context,
	repositoryID string,
) ([]incidentContainerImageIdentity, error) {
	store := NewPostgresContainerImageIdentityStore(s.DB)
	rows, err := store.ListContainerImageIdentities(ctx, ContainerImageIdentityFilter{
		RepositoryID: repositoryID,
		Limit:        incidentRuntimeEvidenceLimit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list incident container image identities: %w", err)
	}
	images := make([]incidentContainerImageIdentity, 0, len(rows))
	for _, row := range rows {
		images = append(images, incidentContainerImageIdentity{
			FactID:           row.IdentityID,
			Digest:           row.Digest,
			ImageRef:         row.ImageRef,
			RepositoryID:     row.RepositoryID,
			Outcome:          row.Outcome,
			Reason:           row.Reason,
			IdentityStrength: row.IdentityStrength,
			CanonicalID:      row.CanonicalID,
			EvidenceFactIDs:  row.EvidenceFactIDs,
		})
	}
	return images, nil
}

func (s PostgresIncidentContextStore) readIncidentCICDRunCorrelations(
	ctx context.Context,
	image incidentContainerImageIdentity,
) ([]incidentCICDRunCorrelation, error) {
	if image.Digest != "" {
		store := NewPostgresCICDRunCorrelationStore(s.DB)
		rows, err := store.ListCICDRunCorrelations(ctx, CICDRunCorrelationFilter{
			ArtifactDigest: image.Digest,
			Limit:          incidentRuntimeEvidenceLimit + 1,
		})
		if err != nil {
			return nil, fmt.Errorf("list incident ci/cd run correlations: %w", err)
		}
		return incidentCICDRunCorrelationsFromRows(rows), nil
	}
	if image.ImageRef == "" {
		return nil, nil
	}
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentCICDRunCorrelationsByImageRefQuery,
		image.ImageRef,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident ci/cd run correlations by image ref: %w", err)
	}
	correlations := make([]incidentCICDRunCorrelation, 0, len(rows))
	for _, row := range rows {
		correlations = append(correlations, decodeIncidentCICDRunCorrelation(row))
	}
	return correlations, nil
}

func (s PostgresIncidentContextStore) readIncidentKubernetesCorrelations(
	ctx context.Context,
	image incidentContainerImageIdentity,
) ([]incidentKubernetesCorrelation, error) {
	if image.Digest == "" && image.ImageRef == "" {
		return nil, nil
	}
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentKubernetesCorrelationsByImageQuery,
		image.Digest,
		image.ImageRef,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident kubernetes correlations: %w", err)
	}
	correlations := make([]incidentKubernetesCorrelation, 0, len(rows))
	for _, row := range rows {
		correlations = append(correlations, decodeIncidentKubernetesCorrelation(row))
	}
	return correlations, nil
}

func incidentCICDRunCorrelationsFromRows(
	rows []CICDRunCorrelationRow,
) []incidentCICDRunCorrelation {
	correlations := make([]incidentCICDRunCorrelation, 0, len(rows))
	for _, row := range rows {
		correlations = append(correlations, incidentCICDRunCorrelation{
			FactID:          row.CorrelationID,
			Provider:        row.Provider,
			RunID:           row.RunID,
			RunAttempt:      row.RunAttempt,
			RepositoryID:    row.RepositoryID,
			CommitSHA:       row.CommitSHA,
			Environment:     row.Environment,
			ArtifactDigest:  row.ArtifactDigest,
			ImageRef:        row.ImageRef,
			Outcome:         row.Outcome,
			Reason:          row.Reason,
			ProvenanceOnly:  row.ProvenanceOnly,
			CanonicalTarget: row.CanonicalTarget,
			CorrelationKind: row.CorrelationKind,
			EvidenceFactIDs: row.EvidenceFactIDs,
		})
	}
	return correlations
}

func decodeIncidentCICDRunCorrelation(row incidentContextFactRow) incidentCICDRunCorrelation {
	return incidentCICDRunCorrelation{
		FactID:          row.FactID,
		Provider:        StringVal(row.Payload, "provider"),
		RunID:           StringVal(row.Payload, "run_id"),
		RunAttempt:      StringVal(row.Payload, "run_attempt"),
		RepositoryID:    StringVal(row.Payload, "repository_id"),
		CommitSHA:       StringVal(row.Payload, "commit_sha"),
		Environment:     StringVal(row.Payload, "environment"),
		ArtifactDigest:  StringVal(row.Payload, "artifact_digest"),
		ImageRef:        StringVal(row.Payload, "image_ref"),
		Outcome:         StringVal(row.Payload, "outcome"),
		Reason:          StringVal(row.Payload, "reason"),
		ProvenanceOnly:  BoolVal(row.Payload, "provenance_only"),
		CanonicalTarget: StringVal(row.Payload, "canonical_target"),
		CorrelationKind: StringVal(row.Payload, "correlation_kind"),
		EvidenceFactIDs: StringSliceVal(row.Payload, "evidence_fact_ids"),
	}
}

func decodeIncidentServiceCatalogOperationalLink(
	row incidentContextFactRow,
) incidentServiceCatalogOperationalLink {
	return incidentServiceCatalogOperationalLink{
		FactID:    row.FactID,
		Provider:  StringVal(row.Payload, "provider"),
		EntityRef: StringVal(row.Payload, "entity_ref"),
		LinkType:  StringVal(row.Payload, "link_type"),
		Title:     StringVal(row.Payload, "title"),
		URL:       StringVal(row.Payload, "url"),
	}
}

func decodeIncidentKubernetesCorrelation(row incidentContextFactRow) incidentKubernetesCorrelation {
	return incidentKubernetesCorrelation{
		FactID:                 row.FactID,
		ClusterID:              StringVal(row.Payload, "cluster_id"),
		WorkloadObjectID:       StringVal(row.Payload, "workload_object_id"),
		Namespace:              StringVal(row.Payload, "namespace"),
		WorkloadName:           StringVal(row.Payload, "workload_name"),
		ImageRef:               StringVal(row.Payload, "image_ref"),
		SourceDigest:           StringVal(row.Payload, "source_digest"),
		JoinMode:               StringVal(row.Payload, "join_mode"),
		Outcome:                StringVal(row.Payload, "outcome"),
		Reason:                 StringVal(row.Payload, "reason"),
		ProvenanceOnly:         BoolVal(row.Payload, "provenance_only"),
		CandidateSourceDigests: StringSliceVal(row.Payload, "candidate_source_digests"),
		EvidenceFactIDs:        StringSliceVal(row.Payload, "evidence_fact_ids"),
	}
}

func ambiguousIncidentServiceLinkEdge(
	links []incidentServiceCatalogOperationalLink,
) IncidentContextEvidenceEdge {
	candidates := make([]IncidentContextEvidenceCandidate, 0, len(links))
	for _, link := range links {
		candidates = append(candidates, IncidentContextEvidenceCandidate{
			ID:     link.EntityRef,
			Label:  firstNonEmpty(link.Title, link.EntityRef),
			URL:    link.URL,
			Reason: "PagerDuty service URL matched multiple service-catalog operational links",
		})
	}
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotDeployable,
		TruthLabel:  IncidentTruthAmbiguous,
		Explanation: "PagerDuty service URL matched multiple service-catalog operational links; pass stronger service mapping evidence before selecting a deployable",
		Candidates:  candidates,
	}
}
