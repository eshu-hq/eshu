// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// Flux cross-repo URL resolution outcomes (issue #5483 C2). This is the
// closed, bounded label set discoverStructuredFluxEvidence tallies into
// FluxCrossRepoURLResolutionStats and the eshu_dp_flux_cross_repo_url_resolution_total
// metric's outcome label; producers must use exactly these four values.
const (
	// FluxCrossRepoURLResolutionOutcomeLinked is the outcome when a
	// GitRepository spec.url normalizes to exactly one OTHER catalog
	// repository's RemoteURL: a DEPLOYS_FROM EvidenceFact is emitted.
	FluxCrossRepoURLResolutionOutcomeLinked = "linked"
	// FluxCrossRepoURLResolutionOutcomeUnresolved is the outcome when no
	// catalog repository's RemoteURL matches: the url may name a repository
	// Eshu has not indexed, or one outside the catalog snapshot.
	FluxCrossRepoURLResolutionOutcomeUnresolved = "unresolved"
	// FluxCrossRepoURLResolutionOutcomeAmbiguous is the outcome when 2+
	// catalog repositories share the same normalized RemoteURL. This is
	// structurally near-impossible (RepoID derives from the normalized URL
	// and the catalog dedupes by RepoID), but the never-fabricate guard
	// tallies it rather than guessing.
	FluxCrossRepoURLResolutionOutcomeAmbiguous = "ambiguous"
	// FluxCrossRepoURLResolutionOutcomeSelf is the outcome when the matched
	// repository IS the repository hosting the GitRepository manifest: a
	// same-repo Flux source is already covered by the C1/PR B
	// RECONCILES_FROM edge, so no cross-repo edge is emitted.
	FluxCrossRepoURLResolutionOutcomeSelf = "self"
)

// FluxCrossRepoURLResolutionStats tallies discoverStructuredFluxEvidence's
// strict-URL resolution outcomes for one discovery pass. Every GitRepository
// url considered lands in exactly one counter; an unresolved or ambiguous url
// is an honest non-link, never a guessed edge.
type FluxCrossRepoURLResolutionStats struct {
	Linked     int
	Unresolved int
	Ambiguous  int
	Self       int
}

func (s *FluxCrossRepoURLResolutionStats) record(outcome string) {
	if s == nil {
		return
	}
	switch outcome {
	case FluxCrossRepoURLResolutionOutcomeLinked:
		s.Linked++
	case FluxCrossRepoURLResolutionOutcomeUnresolved:
		s.Unresolved++
	case FluxCrossRepoURLResolutionOutcomeAmbiguous:
		s.Ambiguous++
	case FluxCrossRepoURLResolutionOutcomeSelf:
		s.Self++
	}
}

// discoverStructuredFluxEvidence resolves a Flux GitRepository's spec.url
// (captured in parsed_file_data.flux_git_repositories by #5360 PR A) to a
// Repository node identity via STRICT repositoryidentity.NormalizeRemoteURL
// equality against each catalog entry's RemoteURL -- never the fuzzy
// alias/token matcher matchCatalog uses. This is the cross-repo lineage half
// of #5483: a Flux Kustomization or HelmRelease that reconciles from a
// GitRepository whose spec.url names a DIFFERENT repository than the one
// hosting the manifest.
//
// Never-fabricate guarantees, one outcome per GitRepository url considered:
//   - exactly one catalog entry's RemoteURL normalizes equal to spec.url, and
//     that entry is NOT the source repository -> one DEPLOYS_FROM
//     EvidenceFact, tallied linked.
//   - the sole match IS the source repository (a same-repo GitRepository CR)
//     -> no evidence emitted, tallied self; same-repo lineage is the C1/PR B
//     RECONCILES_FROM edge, not a cross-repo one.
//   - no catalog entry matches -> no evidence emitted, tallied unresolved.
//   - 2+ catalog entries match -> no evidence emitted, tallied ambiguous,
//     never guessed.
//
// GitRepository urls only: parsed_file_data's flux_git_repositories bucket
// never carries an OCIRepository's oci:// url (that lives in the sibling
// flux_oci_repositories bucket, which this function does not read), so an
// OCIRepository registry reference is never resolved as a Repository node.
// discoverStructuredFluxEvidence reads the parsed_file_data
// flux_git_repositories inner key through the typed
// factschema.DecodeParsedFileDataFluxGitRepositories accessor (issue #5445
// slice 1) rather than a raw map lookup. The accessor skips a malformed row
// rather than failing the whole bucket, so a decode error here is always
// nil in practice; the error return is ignored deliberately, matching the
// pre-typing raw-map read's silent tolerance of an absent/wrong-shape
// bucket.
func discoverStructuredFluxEvidence(
	sourceRepoID, filePath string,
	parsedFileData map[string]any,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	stats *DiscoveryStats,
) []EvidenceFact {
	gitRepositories, _ := factschema.DecodeParsedFileDataFluxGitRepositories(parsedFileData)

	var evidence []EvidenceFact
	for _, row := range gitRepositories {
		fact, ok := discoverFluxGitRepositoryEvidence(sourceRepoID, filePath, row, matcher, seen, stats)
		if ok {
			evidence = append(evidence, fact)
		}
	}

	return evidence
}

// discoverFluxGitRepositoryEvidence resolves one flux_git_repositories row.
// It always records exactly one outcome into stats (when stats is non-nil);
// ok is true only for the linked outcome, which is the one outcome that
// produces an EvidenceFact.
func discoverFluxGitRepositoryEvidence(
	sourceRepoID, filePath string,
	row codegraphv1.FluxGitRepository,
	matcher *catalogMatcher,
	seen map[evidenceKey]struct{},
	stats *DiscoveryStats,
) (EvidenceFact, bool) {
	url := strings.TrimSpace(row.URL)
	if url == "" {
		return EvidenceFact{}, false
	}
	normalizedURL := repositoryidentity.NormalizeRemoteURL(url)
	if normalizedURL == "" {
		return EvidenceFact{}, false
	}

	matches := matcher.remoteURLMatches(normalizedURL)
	switch len(matches) {
	case 0:
		stats.recordFluxCrossRepoURLResolution(FluxCrossRepoURLResolutionOutcomeUnresolved)
		return EvidenceFact{}, false
	case 1:
		target := matches[0]
		if target.RepoID == sourceRepoID {
			stats.recordFluxCrossRepoURLResolution(FluxCrossRepoURLResolutionOutcomeSelf)
			return EvidenceFact{}, false
		}

		fluxGitRepositoryName := strings.TrimSpace(row.Name)
		fluxGitRepositoryNamespace := strings.TrimSpace(row.Namespace)
		key := evidenceKey{
			EvidenceKind:   EvidenceKindFluxGitRepositorySource,
			SourceRepoID:   sourceRepoID,
			TargetRepoID:   target.RepoID,
			Path:           filePath,
			MatchedValue:   normalizedURL,
			SourceEntityID: strings.Join([]string{"FluxGitRepository", fluxGitRepositoryNamespace, fluxGitRepositoryName}, "\x00"),
		}
		stats.recordFluxCrossRepoURLResolution(FluxCrossRepoURLResolutionOutcomeLinked)
		if _, exists := seen[key]; exists {
			return EvidenceFact{}, false
		}
		seen[key] = struct{}{}

		return EvidenceFact{
			EvidenceKind:     EvidenceKindFluxGitRepositorySource,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     sourceRepoID,
			SourceEntityID:   key.SourceEntityID,
			TargetRepoID:     target.RepoID,
			Confidence:       DefaultConfidenceRegistry.ConfidenceFor(EvidenceKindFluxGitRepositorySource),
			Rationale:        "a Flux GitRepository spec.url resolves by exact normalized-URL identity to exactly one catalog repository",
			Details: map[string]any{
				"path":                          filePath,
				"extractor":                     "flux",
				"flux_git_repository_name":      fluxGitRepositoryName,
				"flux_git_repository_namespace": fluxGitRepositoryNamespace,
				"url":                           url,
				"normalized_url":                normalizedURL,
			},
		}, true
	default:
		stats.recordFluxCrossRepoURLResolution(FluxCrossRepoURLResolutionOutcomeAmbiguous)
		return EvidenceFact{}, false
	}
}

// recordFluxCrossRepoURLResolution is a nil-safe DiscoveryStats method so
// discoverFluxGitRepositoryEvidence can be called with a nil stats pointer
// (as every existing DiscoverEvidence caller's evidenceKey path implies)
// without a guard at every call site.
func (s *DiscoveryStats) recordFluxCrossRepoURLResolution(outcome string) {
	if s == nil {
		return
	}
	s.FluxCrossRepoURLResolution.record(outcome)
}
