// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/admissiondecision"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func (h PackageSourceCorrelationHandler) writePackageSourceAdmissionDecisions(
	ctx context.Context,
	intent reducercontract.Intent,
	ownership []PackageSourceCorrelationDecision,
	consumption []PackageConsumptionDecision,
	publication []PackagePublicationDecision,
) error {
	if h.AdmissionDecisionWriter == nil {
		return nil
	}
	now := admissiondecision.AdmissionNow(h.AdmissionDecisionNow)
	writes := make([]admissiondecision.AdmissionDecisionWrite, 0, len(ownership)+len(consumption)+len(publication))
	for _, decision := range ownership {
		writes = append(writes, packageOwnershipAdmissionDecision(intent, decision, now))
	}
	for _, decision := range consumption {
		writes = append(writes, packageConsumptionAdmissionDecision(intent, decision, now))
	}
	for _, decision := range publication {
		writes = append(writes, packagePublicationAdmissionDecision(intent, decision, now))
	}
	return admissiondecision.WriteAdmissionDecisions(ctx, h.AdmissionDecisionWriter, writes)
}

func packageOwnershipAdmissionDecision(
	intent reducercontract.Intent,
	source PackageSourceCorrelationDecision,
	now time.Time,
) admissiondecision.AdmissionDecisionWrite {
	state := packageSourceAdmissionState(source.Outcome, source.ProvenanceOnly, source.CanonicalWrites)
	candidateID := admissiondecision.StableAdmissionDecisionID(
		string(reducercontract.DomainPackageSourceCorrelation),
		"ownership",
		source.PackageID,
		source.SourceURL,
		source.RepositoryID,
		strings.Join(source.CandidateRepositoryIDs, ","),
	)
	decision := admissiondecision.NewAdmissionDecision(
		reducercontract.DomainPackageSourceCorrelation,
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
	decision.ConfidenceBucket = admissiondecision.AdmissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = "package_registry_source_hint"
	decision.SourceHandles = packageSourceFactHandles(source.EvidenceFactIDs, intent.ScopeID)
	decision.CanonicalWrite = admissiondecision.AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    PackageOwnershipCorrelationFactKind,
		SkippedReason: "source hint is provenance-only until stronger package ownership evidence exists",
	}
	decision.RecommendedAction = packageSourceAdmissionNextAction(state, "package ownership")
	return admissiondecision.AdmissionDecisionWrite{
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
				"candidate_repository_ids": payloadcore.UniqueSortedStrings(source.CandidateRepositoryIDs),
			},
			now,
		),
	}
}

func packageConsumptionAdmissionDecision(
	intent reducercontract.Intent,
	source PackageConsumptionDecision,
	now time.Time,
) admissiondecision.AdmissionDecisionWrite {
	state := admissiondecision.AdmissionStateMissingEvidence
	if source.CanonicalWrites > 0 {
		state = admissiondecision.AdmissionStateAdmitted
	}
	targetID := admissiondecision.StableAdmissionDecisionID(
		string(reducercontract.DomainPackageSourceCorrelation),
		"consumption",
		source.PackageID,
		source.RepositoryID,
		source.RelativePath,
	)
	canonical := admissiondecision.AdmissionCanonicalWrite{
		Eligible:      source.CanonicalWrites > 0,
		Written:       source.CanonicalWrites > 0,
		TargetKind:    PackageConsumptionCorrelationFactKind,
		TargetID:      targetID,
		SkippedReason: "manifest dependency evidence did not admit canonical consumption",
	}
	if canonical.Written {
		canonical.SkippedReason = ""
	}
	decision := admissiondecision.NewAdmissionDecision(
		reducercontract.DomainPackageSourceCorrelation,
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
	decision.ConfidenceBucket = admissiondecision.AdmissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = "manifest_dependency"
	decision.SourceHandles = packageSourceFactHandles(source.EvidenceFactIDs, intent.ScopeID)
	decision.CanonicalWrite = canonical
	decision.RecommendedAction = packageSourceAdmissionNextAction(state, "package consumption")
	return admissiondecision.AdmissionDecisionWrite{
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
	intent reducercontract.Intent,
	source PackagePublicationDecision,
	now time.Time,
) admissiondecision.AdmissionDecisionWrite {
	state := packageSourceAdmissionState(source.Outcome, source.ProvenanceOnly, source.CanonicalWrites)
	candidateID := admissiondecision.StableAdmissionDecisionID(
		string(reducercontract.DomainPackageSourceCorrelation),
		"publication",
		source.PackageID,
		source.VersionID,
		source.SourceHintFactID,
	)
	decision := admissiondecision.NewAdmissionDecision(
		reducercontract.DomainPackageSourceCorrelation,
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
	decision.ConfidenceBucket = admissiondecision.AdmissionConfidenceBucket(decision.ConfidenceScore)
	decision.ConfidenceBasis = "package_registry_publication_hint"
	decision.SourceHandles = packageSourceFactHandles(source.EvidenceFactIDs, intent.ScopeID)
	decision.CanonicalWrite = admissiondecision.AdmissionCanonicalWrite{
		Eligible:      false,
		Written:       false,
		TargetKind:    PackagePublicationCorrelationFactKind,
		SkippedReason: "publication hint is provenance-only until release or build evidence exists",
	}
	decision.RecommendedAction = packageSourceAdmissionNextAction(state, "package publication")
	return admissiondecision.AdmissionDecisionWrite{
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
				"candidate_repository_ids": payloadcore.UniqueSortedStrings(source.CandidateRepositoryIDs),
			},
			now,
		),
	}
}

func packageSourceAdmissionState(
	outcome PackageSourceCorrelationOutcome,
	provenanceOnly bool,
	canonicalWrites int,
) admissiondecision.AdmissionState {
	if canonicalWrites > 0 && !provenanceOnly {
		return admissiondecision.AdmissionStateAdmitted
	}
	switch outcome {
	case PackageSourceCorrelationAmbiguous:
		return admissiondecision.AdmissionStateAmbiguous
	case PackageSourceCorrelationStale:
		return admissiondecision.AdmissionStateStale
	case PackageSourceCorrelationRejected:
		return admissiondecision.AdmissionStateRejected
	default:
		return admissiondecision.AdmissionStateMissingEvidence
	}
}

func packageSourceFactHandles(factIDs []string, scopeID string) []admissiondecision.AdmissionDecisionSourceHandle {
	ids := payloadcore.UniqueSortedStrings(factIDs)
	handles := make([]admissiondecision.AdmissionDecisionSourceHandle, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		handles = append(handles, admissiondecision.AdmissionDecisionSourceHandle{
			Kind:    "fact_record",
			ID:      id,
			ScopeID: scopeID,
		})
	}
	return handles
}

func packageSourceDecisionEvidence(
	decision admissiondecision.AdmissionDecision,
	factIDs []string,
	evidenceKind string,
	detail map[string]any,
	now time.Time,
) []admissiondecision.AdmissionDecisionEvidence {
	ids := payloadcore.UniqueSortedStrings(factIDs)
	if len(ids) == 0 {
		return []admissiondecision.AdmissionDecisionEvidence{
			admissiondecision.NewAdmissionDecisionEvidence(decision, decision.DecisionID, evidenceKind, detail, now),
		}
	}
	rows := make([]admissiondecision.AdmissionDecisionEvidence, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, admissiondecision.NewAdmissionDecisionEvidence(decision, id, evidenceKind, detail, now))
	}
	return rows
}

func packageSourceAdmissionConfidence(state admissiondecision.AdmissionState) float64 {
	switch state {
	case admissiondecision.AdmissionStateAdmitted:
		return 1
	case admissiondecision.AdmissionStateMissingEvidence:
		return 0.5
	default:
		return 0
	}
}

func packageSourceAdmissionNextAction(state admissiondecision.AdmissionState, subject string) admissiondecision.AdmissionNextAction {
	switch state {
	case admissiondecision.AdmissionStateAdmitted:
		return admissiondecision.AdmissionNextAction{Action: "none"}
	case admissiondecision.AdmissionStateAmbiguous:
		return admissiondecision.AdmissionNextAction{Action: "disambiguate_" + strings.ReplaceAll(subject, " ", "_")}
	case admissiondecision.AdmissionStateStale:
		return admissiondecision.AdmissionNextAction{Action: "refresh_" + strings.ReplaceAll(subject, " ", "_")}
	case admissiondecision.AdmissionStateRejected:
		return admissiondecision.AdmissionNextAction{Action: "inspect_" + strings.ReplaceAll(subject, " ", "_")}
	default:
		return admissiondecision.AdmissionNextAction{
			Action: "add_stronger_" + strings.ReplaceAll(subject, " ", "_") + "_evidence",
		}
	}
}
