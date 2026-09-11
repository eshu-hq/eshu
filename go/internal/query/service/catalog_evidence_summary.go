// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	serviceCatalogLocalDescriptorEvidenceLimit = 20
	serviceCatalogGitRepositoryScopePrefix     = "git-repository-scope:"
)

// CatalogEvidenceSummary explains catalog evidence separate from
// reducer-owned correlation rows.
type CatalogEvidenceSummary struct {
	LocalDescriptors            CatalogLocalDescriptorEvidence `json:"local_descriptors"`
	ExternalCatalogConfirmation CatalogExternalCatalogEvidence `json:"external_catalog_confirmation"`
	Reason                      string                         `json:"reason,omitempty"`
}

// CatalogLocalDescriptorEvidence summarizes repo-local catalog source
// facts observed in the active repository generation.
type CatalogLocalDescriptorEvidence struct {
	State      string                               `json:"state"`
	Count      int                                  `json:"count"`
	Providers  []string                             `json:"providers,omitempty"`
	SourceURIs []string                             `json:"source_uris,omitempty"`
	Facts      []CatalogLocalDescriptorEvidenceFact `json:"facts,omitempty"`
	Truncated  bool                                 `json:"truncated,omitempty"`
	Reason     string                               `json:"reason,omitempty"`
}

// CatalogLocalDescriptorEvidenceFact is one bounded local descriptor
// fact reference in a service-catalog evidence summary.
type CatalogLocalDescriptorEvidenceFact struct {
	FactID    string `json:"fact_id"`
	FactKind  string `json:"fact_kind"`
	Provider  string `json:"provider,omitempty"`
	EntityRef string `json:"entity_ref,omitempty"`
	SourceURI string `json:"source_uri,omitempty"`
}

// CatalogExternalCatalogEvidence summarizes whether the current page
// contains reducer correlation rows corroborated beyond repo-local descriptor
// scope evidence.
type CatalogExternalCatalogEvidence struct {
	State     string `json:"state"`
	Count     int    `json:"count"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// CatalogLocalDescriptorEvidenceRow is one active source fact that
// proves a repository contains service-catalog descriptor evidence. Its
// canonical home is querycontract with its Filter/Row siblings; this alias
// keeps the family spelling unchanged.
type CatalogLocalDescriptorEvidenceRow = querycontract.ServiceCatalogLocalDescriptorEvidenceRow

// CatalogLocalDescriptorEvidenceStore optionally adds repo-local
// descriptor evidence to a service-catalog correlation store.
type CatalogLocalDescriptorEvidenceStore interface {
	ListServiceCatalogLocalDescriptorEvidence(
		context.Context,
		string,
		int,
	) ([]CatalogLocalDescriptorEvidenceRow, error)
}

func (h *CatalogHandler) serviceCatalogEvidenceSummary(
	ctx context.Context,
	repositoryID string,
	correlations []CatalogCorrelationResult,
	correlationTruncated bool,
) CatalogEvidenceSummary {
	local := h.serviceCatalogLocalDescriptorEvidence(ctx, repositoryID)
	external := serviceCatalogExternalCatalogEvidence(correlations, correlationTruncated, local)
	summary := CatalogEvidenceSummary{
		LocalDescriptors:            local,
		ExternalCatalogConfirmation: external,
	}
	if external.State == "missing" {
		summary.Reason = external.Reason
	}
	return summary
}

func (h *CatalogHandler) serviceCatalogLocalDescriptorEvidence(
	ctx context.Context,
	repositoryID string,
) CatalogLocalDescriptorEvidence {
	if repositoryID == "" {
		return CatalogLocalDescriptorEvidence{
			State:  "not_checked",
			Reason: "repository_scope_required",
		}
	}
	reader, ok := h.Correlations.(CatalogLocalDescriptorEvidenceStore)
	if !ok {
		return CatalogLocalDescriptorEvidence{
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
		return CatalogLocalDescriptorEvidence{
			State:  "unavailable",
			Reason: "local_descriptor_read_failed",
		}
	}
	return serviceCatalogLocalDescriptorEvidenceFromRows(rows)
}

func serviceCatalogLocalDescriptorEvidenceFromRows(
	rows []CatalogLocalDescriptorEvidenceRow,
) CatalogLocalDescriptorEvidence {
	if len(rows) == 0 {
		return CatalogLocalDescriptorEvidence{State: "absent"}
	}

	truncated := len(rows) > serviceCatalogLocalDescriptorEvidenceLimit
	if truncated {
		rows = rows[:serviceCatalogLocalDescriptorEvidenceLimit]
	}
	count := len(rows)

	providerSet := map[string]struct{}{}
	sourceURISet := map[string]struct{}{}
	facts := make([]CatalogLocalDescriptorEvidenceFact, 0, len(rows))
	for _, row := range rows {
		if row.Provider != "" {
			providerSet[row.Provider] = struct{}{}
		}
		if row.SourceURI != "" {
			sourceURISet[row.SourceURI] = struct{}{}
		}
		facts = append(facts, CatalogLocalDescriptorEvidenceFact(row))
	}

	return CatalogLocalDescriptorEvidence{
		State:      "present",
		Count:      count,
		Providers:  sortedServiceCatalogEvidenceKeys(providerSet),
		SourceURIs: sortedServiceCatalogEvidenceKeys(sourceURISet),
		Facts:      facts,
		Truncated:  truncated,
	}
}

func serviceCatalogExternalCatalogEvidence(
	correlations []CatalogCorrelationResult,
	correlationTruncated bool,
	local CatalogLocalDescriptorEvidence,
) CatalogExternalCatalogEvidence {
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
		return CatalogExternalCatalogEvidence{
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
	return CatalogExternalCatalogEvidence{
		State:     "missing",
		Count:     0,
		Truncated: correlationTruncated,
		Reason:    reason,
	}
}

func serviceCatalogCorrelationFromRepoLocalDescriptor(
	correlation CatalogCorrelationResult,
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
