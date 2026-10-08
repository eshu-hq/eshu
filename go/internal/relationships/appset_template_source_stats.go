// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

// ApplicationSet template-source outcomes (issue #7767). This is the closed,
// bounded label set the ApplicationSet extractors tally into
// ApplicationSetTemplateSourceStats and the
// eshu_dp_argocd_applicationset_template_source_total metric's outcome label;
// producers must use exactly these four values, and never a repository name.
//
// The first three outcomes describe a matched template source (one that
// resolves to a catalog repository) and are mutually exclusive for it. The
// fourth, skipped_templated_destination, is a per-matched-repository extra on
// top of deploy_source or template_source_self_reference, not a partition: the
// repository edge was emitted but its destination was templated, so no
// platform edge could be. A template source that matches no catalog
// repository is never counted.
const (
	// ApplicationSetTemplateSourceOutcomeDeploySource is the outcome when a
	// template source resolves to a repository that is neither the control
	// repository nor the generator's config repository, and a deploy-source
	// fact was newly emitted.
	ApplicationSetTemplateSourceOutcomeDeploySource = "deploy_source"
	// ApplicationSetTemplateSourceOutcomeSelfReference is the outcome when the
	// template source is the generator's own config repository (not the
	// control repository), and a control-to-deployed template-source fact was
	// newly emitted instead of a self-loop.
	ApplicationSetTemplateSourceOutcomeSelfReference = "template_source_self_reference"
	// ApplicationSetTemplateSourceOutcomeSkippedControlRepo is the outcome when
	// the template source is the control repository itself: nothing is emitted.
	ApplicationSetTemplateSourceOutcomeSkippedControlRepo = "skipped_control_repo"
	// ApplicationSetTemplateSourceOutcomeSkippedTemplatedDestination is the
	// outcome when a matched deployed repository had a destination whose
	// cluster or server is still a template string, so no destination-platform
	// fact could be emitted for it.
	ApplicationSetTemplateSourceOutcomeSkippedTemplatedDestination = "skipped_templated_destination"
)

// Synthetic evidence kinds that exist only to deduplicate the two skip
// outcomes through the discovery pass's seen map. They are never emitted as an
// EvidenceFact.
const (
	evidenceKeySkippedControlRepo          EvidenceKind = "STATS_SKIPPED_CONTROL_REPO"
	evidenceKeySkippedTemplatedDestination EvidenceKind = "STATS_SKIPPED_TEMPLATED_DESTINATION"
)

// ApplicationSetTemplateSourceStats tallies the ApplicationSet template-source
// outcomes for one discovery pass. Unlike FluxCrossRepoURLResolutionStats,
// which counts every URL it considers, these count distinct facts: the two
// emit outcomes only when a fact is newly emitted, and the two skip outcomes
// once per (control repository, deployed repository, file path). Many overlays
// that name the same repository therefore tally once, matching the facts
// persisted.
type ApplicationSetTemplateSourceStats struct {
	DeploySource                int
	SelfReference               int
	SkippedControlRepo          int
	SkippedTemplatedDestination int
}

func (s *ApplicationSetTemplateSourceStats) record(outcome string) {
	if s == nil {
		return
	}
	switch outcome {
	case ApplicationSetTemplateSourceOutcomeDeploySource:
		s.DeploySource++
	case ApplicationSetTemplateSourceOutcomeSelfReference:
		s.SelfReference++
	case ApplicationSetTemplateSourceOutcomeSkippedControlRepo:
		s.SkippedControlRepo++
	case ApplicationSetTemplateSourceOutcomeSkippedTemplatedDestination:
		s.SkippedTemplatedDestination++
	}
}

// recordApplicationSetTemplateSource is a nil-safe DiscoveryStats method so the
// ApplicationSet extractors can run with a nil stats pointer without a guard at
// every call site.
func (s *DiscoveryStats) recordApplicationSetTemplateSource(outcome string) {
	if s == nil {
		return
	}
	s.ApplicationSetTemplateSource.record(outcome)
}

// recordApplicationSetSkipOnce tallies a skip outcome once per (control
// repository, deployed repository, file path), so many overlays that name the
// same repository count once. It dedupes through the discovery pass's seen map
// with a synthetic key and never emits an EvidenceFact.
func recordApplicationSetSkipOnce(
	stats *DiscoveryStats,
	seen map[evidenceKey]struct{},
	keyKind EvidenceKind,
	outcome, controlRepoID, deployedRepoID, filePath string,
) {
	key := evidenceKey{
		EvidenceKind: keyKind,
		SourceRepoID: controlRepoID,
		TargetRepoID: deployedRepoID,
		Path:         filePath,
	}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	stats.recordApplicationSetTemplateSource(outcome)
}

// appendApplicationSetPlatformEvidence emits a destination-platform fact for
// each literal destination of an ApplicationSet whose template source matched
// deployedRepoID. A destination that names a cluster or server only through a
// template string cannot become a platform fact; that is tallied once as
// skipped_templated_destination. A destination with neither field is simply
// absent and is not counted.
func appendApplicationSetPlatformEvidence(
	controlRepoID, deployedRepoID, filePath string,
	destinations []argocdDestination,
	seen map[evidenceKey]struct{},
	stats *DiscoveryStats,
) []EvidenceFact {
	var evidence []EvidenceFact
	for _, destination := range destinations {
		if destination.name == "" && destination.server == "" {
			continue
		}
		if argocdDestinationPlatformID(destination) == "" {
			recordApplicationSetSkipOnce(
				stats, seen, evidenceKeySkippedTemplatedDestination,
				ApplicationSetTemplateSourceOutcomeSkippedTemplatedDestination,
				controlRepoID, deployedRepoID, filePath,
			)
			continue
		}
		evidence = append(evidence, appendDestinationPlatformEvidence(
			deployedRepoID, filePath, destination, seen,
		)...)
	}
	return evidence
}
