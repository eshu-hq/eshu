// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// providerServiceCandidate groups all applied routing rows that share one
// PagerDuty provider service id. Cross-row repository disagreement at this level
// is itself ambiguity: the same provider id resolving to two repositories cannot
// be a tenant boundary.
type providerServiceCandidate struct {
	provider        string
	providerID      string
	backendKind     string
	locatorHash     string
	providerIDExact bool
	factIDs         []string
}

// BuildIncidentRepositoryCorrelations classifies applied PagerDuty service
// routing rows into durable repository correlation decisions. It groups rows by
// provider service id, resolves each distinct backend locator to its owning
// repository through the supplied resolver, and emits exactly one decision per
// provider service id. Only single-owner resolutions across all of a provider
// id's applied rows produce an edge; blank ids, name-only signals, multi-repo
// disagreement, and unresolved backends stay provenance-only. The resolver is
// consulted once per distinct (backend_kind, locator_hash) and memoized so a
// fan-out of incidents over the same backend does not re-query.
func BuildIncidentRepositoryCorrelations(
	ctx context.Context,
	provider string,
	rows []AppliedPagerDutyServiceRouting,
	resolver BackendRepositoryResolver,
) ([]IncidentRepositoryCorrelationDecision, error) {
	candidates, rejected := groupAppliedRoutingByProviderService(provider, rows)
	resolutions, err := resolveDistinctBackends(ctx, candidates, resolver)
	if err != nil {
		return nil, err
	}
	decisions := make([]IncidentRepositoryCorrelationDecision, 0, len(candidates)+len(rejected))
	decisions = append(decisions, rejected...)
	for _, candidate := range candidates {
		decisions = append(decisions, classifyProviderServiceCandidate(candidate, resolutions))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].ProviderServiceID != decisions[j].ProviderServiceID {
			return decisions[i].ProviderServiceID < decisions[j].ProviderServiceID
		}
		return decisions[i].Reason < decisions[j].Reason
	})
	return decisions, nil
}

// groupAppliedRoutingByProviderService buckets applied rows by their PagerDuty
// provider service id. Rows that cannot anchor a durable edge — a blank provider
// id (only a name fingerprint is present) — are returned as rejected,
// provenance-only decisions rather than silently dropped, so an operator can see
// that the routing existed but was too weak for a tenant boundary. When the same
// provider id appears across rows with disagreeing backend locators the
// candidate is marked so it resolves as ambiguous.
func groupAppliedRoutingByProviderService(
	provider string,
	rows []AppliedPagerDutyServiceRouting,
) ([]providerServiceCandidate, []IncidentRepositoryCorrelationDecision) {
	byID := map[string]*providerServiceCandidate{}
	order := make([]string, 0, len(rows))
	rejected := make([]IncidentRepositoryCorrelationDecision, 0)
	for _, row := range rows {
		providerID := strings.TrimSpace(row.ProviderObjectID)
		if providerID == "" {
			rejected = append(rejected, nameOnlyRejectedDecision(provider, row))
			continue
		}
		existing, ok := byID[providerID]
		if !ok {
			byID[providerID] = &providerServiceCandidate{
				provider:        provider,
				providerID:      providerID,
				backendKind:     strings.TrimSpace(row.BackendKind),
				locatorHash:     strings.TrimSpace(row.LocatorHash),
				providerIDExact: row.ProviderIDExact,
				factIDs:         appendRoutingFactID(nil, row),
			}
			order = append(order, providerID)
			continue
		}
		existing.factIDs = appendRoutingFactID(existing.factIDs, row)
		existing.providerIDExact = existing.providerIDExact && row.ProviderIDExact
		if strings.TrimSpace(row.BackendKind) != existing.backendKind ||
			strings.TrimSpace(row.LocatorHash) != existing.locatorHash {
			// The same provider id applied under two distinct backend locators is
			// itself ambiguous: blank the locator so resolution forces ambiguity.
			existing.backendKind = ""
			existing.locatorHash = ""
		}
	}
	candidates := make([]providerServiceCandidate, 0, len(order))
	for _, id := range order {
		candidates = append(candidates, *byID[id])
	}
	return candidates, rejected
}

func appendRoutingFactID(ids []string, row AppliedPagerDutyServiceRouting) []string {
	id := strings.TrimSpace(row.StableFactKey)
	if id == "" {
		id = strings.TrimSpace(row.FactID)
	}
	if id == "" {
		return ids
	}
	return append(ids, id)
}

func nameOnlyRejectedDecision(
	provider string,
	row AppliedPagerDutyServiceRouting,
) IncidentRepositoryCorrelationDecision {
	return IncidentRepositoryCorrelationDecision{
		Provider:        provider,
		BackendKind:     strings.TrimSpace(row.BackendKind),
		LocatorHash:     strings.TrimSpace(row.LocatorHash),
		Outcome:         IncidentRepositoryCorrelationRejected,
		Reason:          "applied routing carries no provider service id; name fingerprint alone is not a durable tenant boundary",
		ProvenanceOnly:  true,
		EvidenceFactIDs: appendRoutingFactID(nil, row),
	}
}

// resolveDistinctBackends resolves each distinct (backend_kind, locator_hash)
// among the candidates to its owning repository exactly once, memoized by the
// composite key, so a fan-out of provider services over the same backend does
// not re-query the resolver. A candidate with a blanked locator (cross-row
// disagreement) is skipped here and forced ambiguous during classification.
func resolveDistinctBackends(
	ctx context.Context,
	candidates []providerServiceCandidate,
	resolver BackendRepositoryResolver,
) (map[string]BackendRepositoryResolution, error) {
	resolutions := map[string]BackendRepositoryResolution{}
	if resolver == nil {
		return resolutions, nil
	}
	for _, candidate := range candidates {
		if candidate.backendKind == "" || candidate.locatorHash == "" {
			continue
		}
		key := backendResolutionKey(candidate.backendKind, candidate.locatorHash)
		if _, ok := resolutions[key]; ok {
			continue
		}
		resolution, err := resolver.ResolveBackendRepository(ctx, candidate.backendKind, candidate.locatorHash)
		if err != nil {
			return nil, fmt.Errorf(
				"resolve backend repository for %s/%s: %w",
				candidate.backendKind, candidate.locatorHash, err,
			)
		}
		resolutions[key] = resolution
	}
	return resolutions, nil
}

func backendResolutionKey(backendKind, locatorHash string) string {
	return strings.TrimSpace(backendKind) + ":" + strings.TrimSpace(locatorHash)
}

// classifyProviderServiceCandidate turns one provider service candidate into a
// single durable decision. The classification is fail-closed: only a single
// owning repository across an unambiguous backend produces an edge; a blanked
// locator (cross-row disagreement), an ambiguous backend owner, or a missing
// owner all stay provenance-only.
func classifyProviderServiceCandidate(
	candidate providerServiceCandidate,
	resolutions map[string]BackendRepositoryResolution,
) IncidentRepositoryCorrelationDecision {
	base := IncidentRepositoryCorrelationDecision{
		Provider:          candidate.provider,
		ProviderServiceID: candidate.providerID,
		BackendKind:       candidate.backendKind,
		LocatorHash:       candidate.locatorHash,
		EvidenceFactIDs:   uniqueSortedStrings(candidate.factIDs),
		ProvenanceOnly:    true,
	}
	if candidate.backendKind == "" || candidate.locatorHash == "" {
		base.Outcome = IncidentRepositoryCorrelationAmbiguous
		base.Reason = "provider service id applied under more than one distinct Terraform backend locator"
		return base
	}
	resolution, ok := resolutions[backendResolutionKey(candidate.backendKind, candidate.locatorHash)]
	if !ok || resolution.Ambiguous {
		base.Outcome = IncidentRepositoryCorrelationAmbiguous
		base.Reason = "more than one config repository owns the applied Terraform backend locator"
		if resolution.Ambiguous && strings.TrimSpace(resolution.RepositoryID) != "" {
			base.CandidateRepositoryIDs = uniqueSortedStrings([]string{resolution.RepositoryID})
		}
		return base
	}
	repoID := strings.TrimSpace(resolution.RepositoryID)
	if repoID == "" {
		base.Outcome = IncidentRepositoryCorrelationUnresolved
		base.Reason = "no Eshu config repository owns the applied Terraform backend locator"
		return base
	}
	base.RepositoryID = repoID
	base.ProvenanceOnly = false
	if candidate.providerIDExact {
		base.Outcome = IncidentRepositoryCorrelationExact
		base.Reason = "incident provider service id matched an applied Terraform service owned by a single repository"
	} else {
		base.Outcome = IncidentRepositoryCorrelationDerived
		base.Reason = "incident provider service id resolved to an applied Terraform service owned by a single repository after normalization"
	}
	return base
}
