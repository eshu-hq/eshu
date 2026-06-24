// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

type incidentCICDRunCorrelation struct {
	FactID          string
	Provider        string
	RunID           string
	RunAttempt      string
	RepositoryID    string
	CommitSHA       string
	Environment     string
	ArtifactDigest  string
	ImageRef        string
	Outcome         string
	Reason          string
	ProvenanceOnly  bool
	CanonicalTarget string
	CorrelationKind string
	EvidenceFactIDs []string
}

type incidentCICDImageMatch int

const (
	incidentCICDImageNoMatch incidentCICDImageMatch = iota
	incidentCICDImageDigestMatch
	incidentCICDImageRefMatch
)

func buildIncidentBuildCommitEdges(
	correlations []incidentCICDRunCorrelation,
	image incidentContainerImageIdentity,
) []IncidentContextEvidenceEdge {
	candidates := incidentCICDPromotionCandidates(correlations, image)
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) > 1 {
		return []IncidentContextEvidenceEdge{
			{
				Slot:        IncidentSlotBuildDeploy,
				TruthLabel:  IncidentTruthAmbiguous,
				Explanation: "multiple CI/CD run correlations match the incident image; no single build or deploy was selected",
				Candidates:  incidentCICDCandidates(candidates),
			},
			{
				Slot:        IncidentSlotCommit,
				TruthLabel:  IncidentTruthAmbiguous,
				Explanation: "multiple CI/CD run correlations name different possible commits for the incident image",
				Candidates:  incidentCommitCandidates(candidates),
			},
		}
	}
	correlation := candidates[0]
	edges := []IncidentContextEvidenceEdge{{
		Slot:        IncidentSlotBuildDeploy,
		TruthLabel:  incidentCICDTruthLabel(correlation, image),
		Explanation: incidentBuildDeployExplanation(correlation),
		Value: map[string]string{
			"provider":         correlation.Provider,
			"run_id":           correlation.RunID,
			"run_attempt":      correlation.RunAttempt,
			"repository_id":    correlation.RepositoryID,
			"environment":      correlation.Environment,
			"artifact_digest":  correlation.ArtifactDigest,
			"image_ref":        correlation.ImageRef,
			"canonical_target": correlation.CanonicalTarget,
			"correlation_kind": correlation.CorrelationKind,
			"provenance_only":  boolString(correlation.ProvenanceOnly),
		},
		Evidence: []IncidentContextEvidenceRef{
			incidentEvidenceRef("reducer_ci_cd_run_correlation", correlation.FactID, "", correlation.Provider),
		},
	}}
	if correlation.CommitSHA != "" {
		edges = append(edges, IncidentContextEvidenceEdge{
			Slot:        IncidentSlotCommit,
			TruthLabel:  incidentCICDTruthLabel(correlation, image),
			Explanation: incidentCommitExplanation(correlation),
			Value: map[string]string{
				"commit_sha":    correlation.CommitSHA,
				"repository_id": correlation.RepositoryID,
				"provider":      correlation.Provider,
				"run_id":        correlation.RunID,
			},
			Evidence: []IncidentContextEvidenceRef{
				incidentEvidenceRef("reducer_ci_cd_run_correlation", correlation.FactID, "", correlation.Provider),
			},
		})
	}
	return edges
}

func incidentCICDPromotionCandidates(
	correlations []incidentCICDRunCorrelation,
	image incidentContainerImageIdentity,
) []incidentCICDRunCorrelation {
	digestExact := make([]incidentCICDRunCorrelation, 0, len(correlations))
	digestOther := make([]incidentCICDRunCorrelation, 0, len(correlations))
	refExact := make([]incidentCICDRunCorrelation, 0, len(correlations))
	refOther := make([]incidentCICDRunCorrelation, 0, len(correlations))
	for _, correlation := range correlations {
		match := incidentCICDImageMatchKind(correlation, image)
		if match == incidentCICDImageNoMatch {
			continue
		}
		switch strings.TrimSpace(correlation.Outcome) {
		case string(IncidentTruthExact):
			if match == incidentCICDImageDigestMatch {
				digestExact = append(digestExact, correlation)
			} else {
				refExact = append(refExact, correlation)
			}
		case string(IncidentTruthDerived), string(IncidentTruthAmbiguous):
			if match == incidentCICDImageDigestMatch {
				digestOther = append(digestOther, correlation)
			} else {
				refOther = append(refOther, correlation)
			}
		}
	}
	if len(digestExact) > 0 {
		return digestExact
	}
	if len(digestOther) > 0 {
		return digestOther
	}
	if len(refExact) > 0 {
		return refExact
	}
	return refOther
}

func incidentCICDImageMatchKind(
	correlation incidentCICDRunCorrelation,
	image incidentContainerImageIdentity,
) incidentCICDImageMatch {
	if image.Digest != "" && correlation.ArtifactDigest == image.Digest {
		return incidentCICDImageDigestMatch
	}
	if image.ImageRef != "" && correlation.ImageRef == image.ImageRef {
		return incidentCICDImageRefMatch
	}
	return incidentCICDImageNoMatch
}

func incidentCICDTruthLabel(
	correlation incidentCICDRunCorrelation,
	image incidentContainerImageIdentity,
) IncidentTruthLabel {
	label := incidentTruthFromReducerOutcome(correlation.Outcome)
	if label == IncidentTruthExact &&
		incidentCICDImageMatchKind(correlation, image) == incidentCICDImageRefMatch {
		return IncidentTruthDerived
	}
	return label
}

func incidentCICDCandidates(
	correlations []incidentCICDRunCorrelation,
) []IncidentContextEvidenceCandidate {
	candidates := make([]IncidentContextEvidenceCandidate, 0, len(correlations))
	for _, correlation := range correlations {
		candidates = append(candidates, IncidentContextEvidenceCandidate{
			ID:     firstNonEmpty(correlation.RunID, correlation.FactID),
			Label:  firstNonEmpty(correlation.Provider, correlation.CorrelationKind),
			Reason: firstNonEmpty(correlation.Reason, "CI/CD run correlation candidate"),
		})
	}
	return candidates
}

func incidentCommitCandidates(
	correlations []incidentCICDRunCorrelation,
) []IncidentContextEvidenceCandidate {
	candidates := make([]IncidentContextEvidenceCandidate, 0, len(correlations))
	for _, correlation := range correlations {
		candidates = append(candidates, IncidentContextEvidenceCandidate{
			ID:     firstNonEmpty(correlation.CommitSHA, correlation.RunID, correlation.FactID),
			Label:  correlation.CommitSHA,
			Reason: firstNonEmpty(correlation.Reason, "commit candidate from CI/CD run correlation"),
		})
	}
	return candidates
}

func incidentBuildDeployExplanation(correlation incidentCICDRunCorrelation) string {
	if correlation.Reason != "" {
		return correlation.Reason
	}
	if correlation.ArtifactDigest != "" {
		return "CI/CD run correlation matched the incident image digest"
	}
	return "CI/CD run correlation matched the incident image reference"
}

func incidentCommitExplanation(correlation incidentCICDRunCorrelation) string {
	if correlation.ArtifactDigest != "" {
		return "commit came from CI/CD run evidence for the incident image digest"
	}
	return "commit came from CI/CD run evidence for the incident image reference"
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
