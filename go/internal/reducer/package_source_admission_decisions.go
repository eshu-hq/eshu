// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"
)

func (h PackageSourceCorrelationHandler) writePackageSourceAdmissionDecisions(
	ctx context.Context,
	intent Intent,
	ownership []PackageSourceCorrelationDecision,
	consumption []PackageConsumptionDecision,
	publication []PackagePublicationDecision,
) error {
	if h.AdmissionDecisionWriter == nil {
		return nil
	}
	now := admissionNow(h.AdmissionDecisionNow)
	writes := make([]AdmissionDecisionWrite, 0, len(ownership)+len(consumption)+len(publication))
	for _, decision := range ownership {
		writes = append(writes, packageOwnershipAdmissionDecision(intent, decision, now))
	}
	for _, decision := range consumption {
		writes = append(writes, packageConsumptionAdmissionDecision(intent, decision, now))
	}
	for _, decision := range publication {
		writes = append(writes, packagePublicationAdmissionDecision(intent, decision, now))
	}
	return writeAdmissionDecisions(ctx, h.AdmissionDecisionWriter, writes)
}

func packageOwnershipAdmissionDecision(
	intent Intent,
	source PackageSourceCorrelationDecision,
	now time.Time,
) AdmissionDecisionWrite {
	state := packageSourceAdmissionState(source.Outcome, source.ProvenanceOnly, source.CanonicalWrites)
	candidateID := stableAdmissionDecisionID(
		string(DomainPackageSourceCorrelation),
		"ownership",
		source.PackageID,
		source.SourceURL,
		source.RepositoryID,
		strings.Join(source.CandidateRepositoryIDs, ","),
	)
	decision := newAdmissionDecision(
		DomainPackageSourceCorrelation,
		state,
		string(source.Outcome),
		intent.ScopeID,
		intent.GenerationID,
		"package",
		source.PackageID,
		"package_ownership",
		candidateID,
		now,
	)
	decision.ConfidenceScore = packageSourceAdmissionConfidence(state)
	decision.ConfidenceBucket = admissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = "package_registry_source_hint"
	decision.SourceHandles = packageSourceFactHandles(source.EvidenceFactIDs, intent.ScopeID)
	decision.CanonicalWrite = AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    packageOwnershipCorrelationFactKind,
		SkippedReason: "source hint is provenance-only until stronger package ownership evidence exists",
	}
	decision.RecommendedAction = packageSourceAdmissionNextAction(state, "package ownership")
	return AdmissionDecisionWrite{
		Decision: decision,
		Evidence: packageSourceDecisionEvidence(
			decision,
			source.EvidenceFactIDs,
			"package_source_hint",
			map[string]any{
				"hint_kind":                source.HintKind,
				"outcome":                  string(source.Outcome),
				"reason":                   source.Reason,
				"provenance_only":          source.ProvenanceOnly,
				"candidate_repository_ids": uniqueSortedStrings(source.CandidateRepositoryIDs),
			},
			now,
		),
	}
}

func packageConsumptionAdmissionDecision(
	intent Intent,
	source PackageConsumptionDecision,
	now time.Time,
) AdmissionDecisionWrite {
	state := AdmissionStateMissingEvidence
	if source.CanonicalWrites > 0 {
		state = AdmissionStateAdmitted
	}
	targetID := stableAdmissionDecisionID(
		string(DomainPackageSourceCorrelation),
		"consumption",
		source.PackageID,
		source.RepositoryID,
		source.RelativePath,
	)
	canonical := AdmissionCanonicalWrite{
		Eligible:      source.CanonicalWrites > 0,
		Written:       source.CanonicalWrites > 0,
		TargetKind:    packageConsumptionCorrelationFactKind,
		TargetID:      targetID,
		SkippedReason: "manifest dependency evidence did not admit canonical consumption",
	}
	if canonical.Written {
		canonical.SkippedReason = ""
	}
	decision := newAdmissionDecision(
		DomainPackageSourceCorrelation,
		state,
		string(source.Outcome),
		intent.ScopeID,
		intent.GenerationID,
		"repository",
		source.RepositoryID,
		"package_consumption",
		targetID,
		now,
	)
	decision.ConfidenceScore = packageSourceAdmissionConfidence(state)
	decision.ConfidenceBucket = admissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = "manifest_dependency"
	decision.SourceHandles = packageSourceFactHandles(source.EvidenceFactIDs, intent.ScopeID)
	decision.CanonicalWrite = canonical
	decision.RecommendedAction = packageSourceAdmissionNextAction(state, "package consumption")
	return AdmissionDecisionWrite{
		Decision: decision,
		Evidence: packageSourceDecisionEvidence(
			decision,
			source.EvidenceFactIDs,
			"package_manifest_dependency",
			map[string]any{
				"ecosystem":        source.Ecosystem,
				"package_name":     source.PackageName,
				"manifest_section": source.ManifestSection,
				"outcome":          string(source.Outcome),
				"reason":           source.Reason,
			},
			now,
		),
	}
}

func packagePublicationAdmissionDecision(
	intent Intent,
	source PackagePublicationDecision,
	now time.Time,
) AdmissionDecisionWrite {
	state := packageSourceAdmissionState(source.Outcome, source.ProvenanceOnly, source.CanonicalWrites)
	candidateID := stableAdmissionDecisionID(
		string(DomainPackageSourceCorrelation),
		"publication",
		source.PackageID,
		source.VersionID,
		source.SourceHintFactID,
	)
	decision := newAdmissionDecision(
		DomainPackageSourceCorrelation,
		state,
		string(source.Outcome),
		intent.ScopeID,
		intent.GenerationID,
		"package_version",
		source.VersionID,
		"package_publication",
		candidateID,
		now,
	)
	decision.ConfidenceScore = packageSourceAdmissionConfidence(state)
	decision.ConfidenceBucket = admissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = "package_registry_publication_hint"
	decision.SourceHandles = packageSourceFactHandles(source.EvidenceFactIDs, intent.ScopeID)
	decision.CanonicalWrite = AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    packagePublicationCorrelationFactKind,
		SkippedReason: "publication hint is provenance-only until release or build evidence exists",
	}
	decision.RecommendedAction = packageSourceAdmissionNextAction(state, "package publication")
	return AdmissionDecisionWrite{
		Decision: decision,
		Evidence: packageSourceDecisionEvidence(
			decision,
			source.EvidenceFactIDs,
			"package_publication_hint",
			map[string]any{
				"version":                  source.Version,
				"source_hint_kind":         source.SourceHintKind,
				"source_hint_version_id":   source.SourceHintVersionID,
				"outcome":                  string(source.Outcome),
				"reason":                   source.Reason,
				"candidate_repository_ids": uniqueSortedStrings(source.CandidateRepositoryIDs),
			},
			now,
		),
	}
}

func packageSourceAdmissionState(
	outcome PackageSourceCorrelationOutcome,
	provenanceOnly bool,
	canonicalWrites int,
) AdmissionState {
	if canonicalWrites > 0 && !provenanceOnly {
		return AdmissionStateAdmitted
	}
	switch outcome {
	case PackageSourceCorrelationAmbiguous:
		return AdmissionStateAmbiguous
	case PackageSourceCorrelationStale:
		return AdmissionStateStale
	case PackageSourceCorrelationRejected:
		return AdmissionStateRejected
	default:
		return AdmissionStateMissingEvidence
	}
}

func packageSourceFactHandles(factIDs []string, scopeID string) []AdmissionDecisionSourceHandle {
	ids := uniqueSortedStrings(factIDs)
	handles := make([]AdmissionDecisionSourceHandle, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		handles = append(handles, AdmissionDecisionSourceHandle{
			Kind:    "fact_record",
			ID:      id,
			ScopeID: scopeID,
		})
	}
	return handles
}

func packageSourceDecisionEvidence(
	decision AdmissionDecision,
	factIDs []string,
	evidenceKind string,
	detail map[string]any,
	now time.Time,
) []AdmissionDecisionEvidence {
	ids := uniqueSortedStrings(factIDs)
	if len(ids) == 0 {
		return []AdmissionDecisionEvidence{
			admissionDecisionEvidence(decision, decision.DecisionID, evidenceKind, detail, now),
		}
	}
	rows := make([]AdmissionDecisionEvidence, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, admissionDecisionEvidence(decision, id, evidenceKind, detail, now))
	}
	return rows
}

func packageSourceAdmissionConfidence(state AdmissionState) float64 {
	switch state {
	case AdmissionStateAdmitted:
		return 1
	case AdmissionStateMissingEvidence:
		return 0.5
	default:
		return 0
	}
}

func packageSourceAdmissionNextAction(state AdmissionState, subject string) AdmissionNextAction {
	switch state {
	case AdmissionStateAdmitted:
		return AdmissionNextAction{Action: "none"}
	case AdmissionStateAmbiguous:
		return AdmissionNextAction{Action: "disambiguate_" + strings.ReplaceAll(subject, " ", "_")}
	case AdmissionStateStale:
		return AdmissionNextAction{Action: "refresh_" + strings.ReplaceAll(subject, " ", "_")}
	case AdmissionStateRejected:
		return AdmissionNextAction{Action: "inspect_" + strings.ReplaceAll(subject, " ", "_")}
	default:
		return AdmissionNextAction{
			Action: "add_stronger_" + strings.ReplaceAll(subject, " ", "_") + "_evidence",
		}
	}
}
