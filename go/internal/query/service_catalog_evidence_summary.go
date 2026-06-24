// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"
	"strings"
)

const (
	serviceCatalogLocalDescriptorEvidenceLimit = 20
	serviceCatalogGitRepositoryScopePrefix     = "git-repository-scope:"
)

// ServiceCatalogEvidenceSummary explains catalog evidence separate from
// reducer-owned correlation rows.
type ServiceCatalogEvidenceSummary struct {
	LocalDescriptors            ServiceCatalogLocalDescriptorEvidence `json:"local_descriptors"`
	ExternalCatalogConfirmation ServiceCatalogExternalCatalogEvidence `json:"external_catalog_confirmation"`
	Reason                      string                                `json:"reason,omitempty"`
}

// ServiceCatalogLocalDescriptorEvidence summarizes repo-local catalog source
// facts observed in the active repository generation.
type ServiceCatalogLocalDescriptorEvidence struct {
	State      string                                      `json:"state"`
	Count      int                                         `json:"count"`
	Providers  []string                                    `json:"providers,omitempty"`
	SourceURIs []string                                    `json:"source_uris,omitempty"`
	Facts      []ServiceCatalogLocalDescriptorEvidenceFact `json:"facts,omitempty"`
	Truncated  bool                                        `json:"truncated,omitempty"`
	Reason     string                                      `json:"reason,omitempty"`
}

// ServiceCatalogLocalDescriptorEvidenceFact is one bounded local descriptor
// fact reference in a service-catalog evidence summary.
type ServiceCatalogLocalDescriptorEvidenceFact struct {
	FactID    string `json:"fact_id"`
	FactKind  string `json:"fact_kind"`
	Provider  string `json:"provider,omitempty"`
	EntityRef string `json:"entity_ref,omitempty"`
	SourceURI string `json:"source_uri,omitempty"`
}

// ServiceCatalogExternalCatalogEvidence summarizes whether the current page
// contains reducer correlation rows corroborated beyond repo-local descriptor
// scope evidence.
type ServiceCatalogExternalCatalogEvidence struct {
	State     string `json:"state"`
	Count     int    `json:"count"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ServiceCatalogLocalDescriptorEvidenceRow is one active source fact that proves
// a repository contains service-catalog descriptor evidence.
type ServiceCatalogLocalDescriptorEvidenceRow struct {
	FactID    string
	FactKind  string
	Provider  string
	EntityRef string
	SourceURI string
}

// ServiceCatalogLocalDescriptorEvidenceStore optionally adds repo-local
// descriptor evidence to a service-catalog correlation store.
type ServiceCatalogLocalDescriptorEvidenceStore interface {
	ListServiceCatalogLocalDescriptorEvidence(
		context.Context,
		string,
		int,
	) ([]ServiceCatalogLocalDescriptorEvidenceRow, error)
}

func (h *ServiceCatalogHandler) serviceCatalogEvidenceSummary(
	ctx context.Context,
	repositoryID string,
	correlations []ServiceCatalogCorrelationResult,
	correlationTruncated bool,
) ServiceCatalogEvidenceSummary {
	local := h.serviceCatalogLocalDescriptorEvidence(ctx, repositoryID)
	external := serviceCatalogExternalCatalogEvidence(correlations, correlationTruncated, local)
	summary := ServiceCatalogEvidenceSummary{
		LocalDescriptors:            local,
		ExternalCatalogConfirmation: external,
	}
	if external.State == "missing" {
		summary.Reason = external.Reason
	}
	return summary
}

func (h *ServiceCatalogHandler) serviceCatalogLocalDescriptorEvidence(
	ctx context.Context,
	repositoryID string,
) ServiceCatalogLocalDescriptorEvidence {
	if repositoryID == "" {
		return ServiceCatalogLocalDescriptorEvidence{
			State:  "not_checked",
			Reason: "repository_scope_required",
		}
	}
	reader, ok := h.Correlations.(ServiceCatalogLocalDescriptorEvidenceStore)
	if !ok {
		return ServiceCatalogLocalDescriptorEvidence{
			State:  "unavailable",
			Reason: "local_descriptor_store_unavailable",
		}
	}
	rows, err := reader.ListServiceCatalogLocalDescriptorEvidence(
		ctx,
		repositoryID,
		serviceCatalogLocalDescriptorEvidenceLimit+1,
	)
	if err != nil {
		return ServiceCatalogLocalDescriptorEvidence{
			State:  "unavailable",
			Reason: "local_descriptor_read_failed",
		}
	}
	return serviceCatalogLocalDescriptorEvidenceFromRows(rows)
}

func serviceCatalogLocalDescriptorEvidenceFromRows(
	rows []ServiceCatalogLocalDescriptorEvidenceRow,
) ServiceCatalogLocalDescriptorEvidence {
	if len(rows) == 0 {
		return ServiceCatalogLocalDescriptorEvidence{State: "absent"}
	}

	truncated := len(rows) > serviceCatalogLocalDescriptorEvidenceLimit
	if truncated {
		rows = rows[:serviceCatalogLocalDescriptorEvidenceLimit]
	}
	count := len(rows)

	providerSet := map[string]struct{}{}
	sourceURISet := map[string]struct{}{}
	facts := make([]ServiceCatalogLocalDescriptorEvidenceFact, 0, len(rows))
	for _, row := range rows {
		if row.Provider != "" {
			providerSet[row.Provider] = struct{}{}
		}
		if row.SourceURI != "" {
			sourceURISet[row.SourceURI] = struct{}{}
		}
		facts = append(facts, ServiceCatalogLocalDescriptorEvidenceFact(row))
	}

	return ServiceCatalogLocalDescriptorEvidence{
		State:      "present",
		Count:      count,
		Providers:  sortedServiceCatalogEvidenceKeys(providerSet),
		SourceURIs: sortedServiceCatalogEvidenceKeys(sourceURISet),
		Facts:      facts,
		Truncated:  truncated,
	}
}

func serviceCatalogExternalCatalogEvidence(
	correlations []ServiceCatalogCorrelationResult,
	correlationTruncated bool,
	local ServiceCatalogLocalDescriptorEvidence,
) ServiceCatalogExternalCatalogEvidence {
	externalCount := 0
	localCorrelationCount := 0
	ambiguousLocal := false
	for _, correlation := range correlations {
		if serviceCatalogCorrelationFromRepoLocalDescriptor(correlation) {
			localCorrelationCount++
			if correlation.Outcome == "ambiguous" ||
				correlation.Outcome == "unresolved" ||
				correlation.Outcome == "stale" ||
				correlation.Outcome == "rejected" {
				ambiguousLocal = true
			}
			continue
		}
		if correlation.ProvenanceOnly {
			continue
		}
		if correlation.Outcome == "exact" || correlation.Outcome == "derived" {
			externalCount++
		}
	}
	if externalCount > 0 {
		return ServiceCatalogExternalCatalogEvidence{
			State:     "present",
			Count:     externalCount,
			Truncated: correlationTruncated,
		}
	}

	reason := "catalog_correlation_missing"
	switch {
	case ambiguousLocal:
		reason = "local_descriptor_ambiguous"
	case localCorrelationCount > 0:
		reason = "local_descriptor_without_external_confirmation"
	case local.State == "present":
		reason = "local_descriptor_without_catalog_correlation"
	case local.State == "absent":
		reason = "no_service_catalog_evidence_found"
	case local.State == "unavailable":
		reason = "local_descriptor_check_unavailable"
	case local.State == "not_checked":
		reason = "repository_scope_required"
	}
	return ServiceCatalogExternalCatalogEvidence{
		State:     "missing",
		Count:     0,
		Truncated: correlationTruncated,
		Reason:    reason,
	}
}

func serviceCatalogCorrelationFromRepoLocalDescriptor(
	correlation ServiceCatalogCorrelationResult,
) bool {
	return strings.HasPrefix(correlation.Reason, "repo-local catalog descriptor scope")
}

func serviceCatalogGitRepositoryScopeID(repositoryID string) string {
	return serviceCatalogGitRepositoryScopePrefix + repositoryID
}

func sortedServiceCatalogEvidenceKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}
