// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	incidentsql "github.com/eshu-hq/eshu/go/internal/query/incident/sql"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

const incidentRuntimeEvidenceLimit = 25

func (s PostgresIncidentContextStore) readIncidentRuntimeEvidence(
	ctx context.Context,
	incident model.IncidentContextIncident,
) ([]model.IncidentContextEvidenceEdge, error) {
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
		return []model.IncidentContextEvidenceEdge{ambiguousIncidentServiceLinkEdge(links)}, nil
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
		incidentsql.ListServiceCatalogOperationalLinksQuery,
		serviceURL,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident service catalog links: %w", err)
	}
	links := make([]incidentServiceCatalogOperationalLink, 0, len(rows))
	for _, row := range rows {
		link, ok := decodeIncidentServiceCatalogOperationalLink(row)
		if !ok {
			continue
		}
		links = append(links, link)
	}
	return links, nil
}

func (s PostgresIncidentContextStore) readIncidentServiceCatalogCorrelations(
	ctx context.Context,
	link incidentServiceCatalogOperationalLink,
) ([]incidentServiceCatalogCorrelation, error) {
	if s.catalog == nil {
		return nil, fmt.Errorf("incident service catalog store is required")
	}
	rows, err := s.catalog.ListServiceCatalogCorrelations(ctx, querycontract.ServiceCatalogCorrelationFilter{
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
	if s.images == nil {
		return nil, fmt.Errorf("incident container image store is required")
	}
	rows, err := s.images.ListContainerImageIdentities(ctx, supplychain.ContainerImageIdentityFilter{
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
		if s.cicd == nil {
			return nil, fmt.Errorf("incident CI/CD run correlation store is required")
		}
		rows, err := s.cicd.ListCICDRunCorrelations(ctx, querycontract.CICDRunCorrelationFilter{
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
		incidentsql.ListCICDRunCorrelationsByImageRefQuery,
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
		incidentsql.ListKubernetesCorrelationsByImageQuery,
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
	rows []querycontract.CICDRunCorrelationRow,
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

// decodeIncidentCICDRunCorrelation decodes one reducer_ci_cd_run_correlation
// fact row into the read model's incidentCICDRunCorrelation via raw payload
// lookups. reducer_ci_cd_run_correlation is a REDUCER-DERIVED fact
// (go/internal/reducer/cicdrun/ci_cd_run_correlation_writer.go writes it
// directly, not through sdk/go/factschema) — it has no factschema.FactKind*
// constant and no Decode* seam, so it is out of scope for the #4794 W2a typed-decode
// conversion (which only converts collector-emitted fact kinds that already
// have a contracts-module seam). Converting reducer-derived reducer_* kinds to
// a typed contract is tracked separately (see the reducer-derived fact
// governance decision, PR #4809).
func decodeIncidentCICDRunCorrelation(row incidentContextFactRow) incidentCICDRunCorrelation {
	return incidentCICDRunCorrelation{
		FactID:          row.FactID,
		Provider:        querycontract.StringVal(row.Payload, "provider"),
		RunID:           querycontract.StringVal(row.Payload, "run_id"),
		RunAttempt:      querycontract.StringVal(row.Payload, "run_attempt"),
		RepositoryID:    querycontract.StringVal(row.Payload, "repository_id"),
		CommitSHA:       querycontract.StringVal(row.Payload, "commit_sha"),
		Environment:     querycontract.StringVal(row.Payload, "environment"),
		ArtifactDigest:  querycontract.StringVal(row.Payload, "artifact_digest"),
		ImageRef:        querycontract.StringVal(row.Payload, "image_ref"),
		Outcome:         querycontract.StringVal(row.Payload, "outcome"),
		Reason:          querycontract.StringVal(row.Payload, "reason"),
		ProvenanceOnly:  querycontract.BoolVal(row.Payload, "provenance_only"),
		CanonicalTarget: querycontract.StringVal(row.Payload, "canonical_target"),
		CorrelationKind: querycontract.StringVal(row.Payload, "correlation_kind"),
		EvidenceFactIDs: querycontract.StringSliceVal(row.Payload, "evidence_fact_ids"),
	}
}

// decodeIncidentServiceCatalogOperationalLink decodes one
// service_catalog.operational_link fact row through the typed
// sdk/go/factschema/servicecatalog/v1 seam (decodeServiceCatalogOperationalLink)
// and shapes it into the read model's incidentServiceCatalogOperationalLink.
// Every field of servicecatalogv1.OperationalLink is optional, so ok is false
// only for an unsupported schema major, never a missing field.
func decodeIncidentServiceCatalogOperationalLink(
	row incidentContextFactRow,
) (incidentServiceCatalogOperationalLink, bool) {
	link, err := decodeServiceCatalogOperationalLink(incidentContextDecodeInput{
		FactID: row.FactID, SchemaVersion: row.SchemaVersion, Payload: row.Payload,
	})
	if err != nil {
		logIncidentContextDecodeDrop(err)
		return incidentServiceCatalogOperationalLink{}, false
	}
	return incidentServiceCatalogOperationalLink{
		FactID:    row.FactID,
		Provider:  workItemDerefString(link.Provider),
		EntityRef: workItemDerefString(link.EntityRef),
		LinkType:  workItemDerefString(link.LinkType),
		Title:     workItemDerefString(link.Title),
		URL:       workItemDerefString(link.URL),
	}, true
}

// decodeIncidentKubernetesCorrelation decodes one reducer_kubernetes_correlation
// fact row into the read model's incidentKubernetesCorrelation via raw payload
// lookups. reducer_kubernetes_correlation is a REDUCER-DERIVED fact
// (go/internal/reducer/kubernetescorrelation/kubernetes_correlation_writer.go writes it directly,
// not through sdk/go/factschema) — it has no factschema.FactKind* constant and
// no Decode* seam, so it is out of scope for the #4794 W2a typed-decode
// conversion for the same reason as decodeIncidentCICDRunCorrelation above.
func decodeIncidentKubernetesCorrelation(row incidentContextFactRow) incidentKubernetesCorrelation {
	return incidentKubernetesCorrelation{
		FactID:                 row.FactID,
		ClusterID:              querycontract.StringVal(row.Payload, "cluster_id"),
		WorkloadObjectID:       querycontract.StringVal(row.Payload, "workload_object_id"),
		Namespace:              querycontract.StringVal(row.Payload, "namespace"),
		WorkloadName:           querycontract.StringVal(row.Payload, "workload_name"),
		ImageRef:               querycontract.StringVal(row.Payload, "image_ref"),
		SourceDigest:           querycontract.StringVal(row.Payload, "source_digest"),
		JoinMode:               querycontract.StringVal(row.Payload, "join_mode"),
		Outcome:                querycontract.StringVal(row.Payload, "outcome"),
		Reason:                 querycontract.StringVal(row.Payload, "reason"),
		ProvenanceOnly:         querycontract.BoolVal(row.Payload, "provenance_only"),
		CandidateSourceDigests: querycontract.StringSliceVal(row.Payload, "candidate_source_digests"),
		EvidenceFactIDs:        querycontract.StringSliceVal(row.Payload, "evidence_fact_ids"),
	}
}

func ambiguousIncidentServiceLinkEdge(
	links []incidentServiceCatalogOperationalLink,
) model.IncidentContextEvidenceEdge {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(links))
	for _, link := range links {
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     link.EntityRef,
			Label:  querycontract.FirstNonEmpty(link.Title, link.EntityRef),
			URL:    link.URL,
			Reason: "PagerDuty service URL matched multiple service-catalog operational links",
		})
	}
	return model.IncidentContextEvidenceEdge{
		Slot:        model.IncidentSlotDeployable,
		TruthLabel:  model.IncidentTruthAmbiguous,
		Explanation: "PagerDuty service URL matched multiple service-catalog operational links; pass stronger service mapping evidence before selecting a deployable",
		Candidates:  candidates,
	}
}
