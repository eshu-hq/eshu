// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
)

func classifyServiceCatalogEntity(
	entity serviceCatalogEntityEvidence,
	index serviceCatalogCorrelationIndex,
) ServiceCatalogCorrelationDecision {
	key := entity.key()
	decision := serviceCatalogBaseDecision(entity, index.ownership[key])
	links := index.repoLinks[key]
	if len(links) == 0 {
		if entity.sourceRepositoryID != "" {
			return classifyRepoLocalServiceCatalogEntity(entity, decision, index.repositoryLookup)
		}
		decision.Outcome = ServiceCatalogCorrelationUnresolved
		decision.Reason = "catalog entity has no repository link evidence"
		decision.DriftStatus = "missing"
		return decision
	}

	activeMatches, staleMatches, rejectedLinks := matchServiceCatalogRepositories(links, index.repositoryLookup)
	decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, serviceCatalogRepositoryLinkFactIDs(links)...)
	switch len(activeMatches) {
	case 0:
		if len(staleMatches) > 0 {
			decision.Outcome = ServiceCatalogCorrelationStale
			decision.Reason = "catalog repository link matched only tombstoned repository facts"
			decision.CandidateRepositoryIDs = serviceCatalogRepositoryIDs(staleMatches)
			decision.DriftStatus = "stale"
			return decision
		}
		if rejectedLinks == len(links) {
			decision.Outcome = ServiceCatalogCorrelationRejected
			decision.Reason = "catalog repository link lacks URL or canonical repository id; name-only links cannot prove ownership"
			decision.DriftStatus = "rejected"
			return decision
		}
		decision.Outcome = ServiceCatalogCorrelationUnresolved
		decision.Reason = "catalog repository link did not match any active repository"
		decision.DriftStatus = "missing"
		return decision
	case 1:
		match := activeMatches[0]
		decision.RepositoryID = match.repository.repositoryID
		decision.Outcome = match.outcome
		decision.Reason = match.reason
		decision.ProvenanceOnly = false
		decision.DriftStatus = "matches"
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.repository.factID)
		if decision.ServiceID == "" {
			decision.ServiceID = match.link.serviceID
		}
		if decision.WorkloadID == "" {
			decision.WorkloadID = match.link.workloadID
		}
		return decision
	default:
		decision.Outcome = ServiceCatalogCorrelationAmbiguous
		decision.Reason = "catalog repository link matches multiple active repository facts"
		decision.CandidateRepositoryIDs = serviceCatalogMatchedRepositoryIDs(activeMatches)
		decision.DriftStatus = "ambiguous"
		for _, match := range activeMatches {
			decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.repository.factID)
		}
		return decision
	}
}

func classifyRepoLocalServiceCatalogEntity(
	entity serviceCatalogEntityEvidence,
	decision ServiceCatalogCorrelationDecision,
	lookup serviceCatalogRepositoryLookup,
) ServiceCatalogCorrelationDecision {
	activeMatches, staleMatches := matchRepoLocalServiceCatalogRepository(entity.sourceRepositoryID, lookup)
	switch len(activeMatches) {
	case 0:
		if len(staleMatches) > 0 {
			decision.Outcome = ServiceCatalogCorrelationStale
			decision.Reason = "repo-local catalog descriptor scope matched only tombstoned repository facts"
			decision.CandidateRepositoryIDs = serviceCatalogRepositoryIDs(staleMatches)
			decision.DriftStatus = "stale"
			for _, match := range staleMatches {
				decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.factID)
			}
			return decision
		}
		decision.Outcome = ServiceCatalogCorrelationUnresolved
		decision.Reason = "repo-local catalog descriptor scope did not match any active repository"
		decision.DriftStatus = "missing"
		return decision
	case 1:
		match := activeMatches[0]
		decision.RepositoryID = match.repositoryID
		if decision.ServiceID == "" {
			decision.ServiceID = serviceCatalogAdmittedServiceID(entity)
		}
		decision.Outcome = ServiceCatalogCorrelationExact
		decision.Reason = "repo-local catalog descriptor scope matches canonical repository identity"
		decision.ProvenanceOnly = false
		decision.DriftStatus = "matches"
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.factID)
		return decision
	default:
		decision.Outcome = ServiceCatalogCorrelationAmbiguous
		decision.Reason = "repo-local catalog descriptor scope matches multiple active repository facts"
		decision.CandidateRepositoryIDs = serviceCatalogRepositoryIDs(activeMatches)
		decision.DriftStatus = "ambiguous"
		for _, match := range activeMatches {
			decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.factID)
		}
		return decision
	}
}

func serviceCatalogAdmittedServiceID(entity serviceCatalogEntityEvidence) string {
	if strings.EqualFold(strings.TrimSpace(entity.entityType), "service") {
		return entity.entityRef
	}
	return ""
}

func serviceCatalogBaseDecision(
	entity serviceCatalogEntityEvidence,
	owner serviceCatalogOwnershipEvidence,
) ServiceCatalogCorrelationDecision {
	return ServiceCatalogCorrelationDecision{
		Provider:        entity.provider,
		EntityRef:       entity.entityRef,
		EntityType:      entity.entityType,
		DisplayName:     entity.displayName,
		ServiceID:       entity.serviceID,
		WorkloadID:      entity.workloadID,
		OwnerRef:        owner.ownerRef,
		Lifecycle:       entity.lifecycle,
		Tier:            entity.tier,
		ProvenanceOnly:  true,
		DriftKind:       "repository",
		EvidenceFactIDs: compactStringSlice(entity.factID, owner.factID),
	}
}

func serviceCatalogSourceRepositoryID(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if !strings.HasPrefix(scopeID, serviceCatalogGitRepositoryScopePrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(scopeID, serviceCatalogGitRepositoryScopePrefix))
}

type serviceCatalogRepositoryMatch struct {
	link       serviceCatalogRepositoryLinkEvidence
	repository serviceCatalogRepositoryEvidence
	outcome    ServiceCatalogCorrelationOutcome
	reason     string
	strength   int
}

func matchServiceCatalogRepositories(
	links []serviceCatalogRepositoryLinkEvidence,
	lookup serviceCatalogRepositoryLookup,
) ([]serviceCatalogRepositoryMatch, []serviceCatalogRepositoryEvidence, int) {
	var active []serviceCatalogRepositoryMatch
	var stale []serviceCatalogRepositoryEvidence
	rejected := 0
	for _, link := range links {
		if link.repositoryID != "" {
			linkActive, linkStale := matchServiceCatalogRepositoryID(link, lookup)
			active = append(active, linkActive...)
			stale = append(stale, linkStale...)
			continue
		}
		if link.repositoryURL == "" {
			rejected++
			continue
		}
		linkActive, linkStale := matchServiceCatalogRepositoryURL(link, lookup)
		active = append(active, linkActive...)
		stale = append(stale, linkStale...)
	}
	return uniqueServiceCatalogRepositoryMatches(active), uniqueServiceCatalogRepositories(stale), rejected
}

func matchServiceCatalogRepositoryID(
	link serviceCatalogRepositoryLinkEvidence,
	lookup serviceCatalogRepositoryLookup,
) ([]serviceCatalogRepositoryMatch, []serviceCatalogRepositoryEvidence) {
	var active []serviceCatalogRepositoryMatch
	var stale []serviceCatalogRepositoryEvidence
	activeRepositories, staleRepositories := lookup.byRepositoryID(link.repositoryID)
	stale = append(stale, staleRepositories...)
	for _, repository := range activeRepositories {
		active = append(active, serviceCatalogRepositoryMatch{
			link:       link,
			repository: repository,
			outcome:    ServiceCatalogCorrelationExact,
			reason:     "catalog repository id matches canonical repository identity",
			strength:   3,
		})
	}
	return active, stale
}

func matchRepoLocalServiceCatalogRepository(
	repositoryID string,
	lookup serviceCatalogRepositoryLookup,
) ([]serviceCatalogRepositoryEvidence, []serviceCatalogRepositoryEvidence) {
	active, stale := lookup.byRepositoryID(repositoryID)
	return uniqueServiceCatalogRepositories(active), uniqueServiceCatalogRepositories(stale)
}

func matchServiceCatalogRepositoryURL(
	link serviceCatalogRepositoryLinkEvidence,
	lookup serviceCatalogRepositoryLookup,
) ([]serviceCatalogRepositoryMatch, []serviceCatalogRepositoryEvidence) {
	linkKey := canonicalPackageSourceURLKey(link.repositoryURL)
	if linkKey == "" {
		return nil, nil
	}
	var active []serviceCatalogRepositoryMatch
	var stale []serviceCatalogRepositoryEvidence
	activeRepositories, staleRepositories := lookup.byCanonicalURL(linkKey)
	stale = append(stale, staleRepositories...)
	for _, repository := range activeRepositories {
		outcome := ServiceCatalogCorrelationDerived
		reason := "catalog repository link matches repository remote after git URL canonicalization"
		strength := 1
		if exactPackageSourceURLMatch(link.repositoryURL, repository.remoteURL) {
			outcome = ServiceCatalogCorrelationExact
			reason = "catalog repository link matches repository remote exactly"
			strength = 2
		}
		active = append(active, serviceCatalogRepositoryMatch{
			link:       link,
			repository: repository,
			outcome:    outcome,
			reason:     reason,
			strength:   strength,
		})
	}
	return active, stale
}

func uniqueServiceCatalogRepositoryMatches(
	matches []serviceCatalogRepositoryMatch,
) []serviceCatalogRepositoryMatch {
	bestByRepo := make(map[string]serviceCatalogRepositoryMatch, len(matches))
	for _, match := range matches {
		key := match.repository.repositoryID
		current, ok := bestByRepo[key]
		if ok && current.strength >= match.strength {
			continue
		}
		bestByRepo[key] = match
	}
	out := make([]serviceCatalogRepositoryMatch, 0, len(bestByRepo))
	for _, match := range bestByRepo {
		out = append(out, match)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].repository.repositoryID < out[j].repository.repositoryID
	})
	return out
}

func uniqueServiceCatalogRepositories(
	repositories []serviceCatalogRepositoryEvidence,
) []serviceCatalogRepositoryEvidence {
	seen := make(map[string]struct{}, len(repositories))
	out := make([]serviceCatalogRepositoryEvidence, 0, len(repositories))
	for _, repository := range repositories {
		if _, ok := seen[repository.repositoryID]; ok {
			continue
		}
		seen[repository.repositoryID] = struct{}{}
		out = append(out, repository)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].repositoryID < out[j].repositoryID
	})
	return out
}

func serviceCatalogRepositoryLinkFactIDs(links []serviceCatalogRepositoryLinkEvidence) []string {
	ids := make([]string, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.factID)
	}
	return ids
}

func serviceCatalogMatchedRepositoryIDs(matches []serviceCatalogRepositoryMatch) []string {
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.repository.repositoryID)
	}
	return uniqueSortedStrings(ids)
}

func serviceCatalogRepositoryIDs(repositories []serviceCatalogRepositoryEvidence) []string {
	ids := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		ids = append(ids, repository.repositoryID)
	}
	return uniqueSortedStrings(ids)
}
