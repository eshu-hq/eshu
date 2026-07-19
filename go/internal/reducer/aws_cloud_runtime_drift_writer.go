// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

const awsCloudRuntimeDriftFactKind = facts.ReducerAWSCloudRuntimeDriftFindingFactKind

// PostgresAWSCloudRuntimeDriftWriter persists admitted AWS runtime drift
// findings into the shared fact store.
type PostgresAWSCloudRuntimeDriftWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteAWSCloudRuntimeDriftFindings stores one durable fact per admitted
// finding. The fact id is stable by candidate identity so reducer retries and
// replays upsert the same row instead of duplicating findings.
func (w PostgresAWSCloudRuntimeDriftWriter) WriteAWSCloudRuntimeDriftFindings(
	ctx context.Context,
	write AWSCloudRuntimeDriftWrite,
) (AWSCloudRuntimeDriftWriteResult, error) {
	if w.DB == nil {
		return AWSCloudRuntimeDriftWriteResult{}, fmt.Errorf("aws cloud runtime drift database is required")
	}

	now := reducerWriterNow(w.Now)
	canonicalIDs := make([]string, 0, len(write.Candidates))
	rows := make([]reducerFactVersionedRow, 0, len(write.Candidates))
	for _, candidate := range write.Candidates {
		canonicalID := canonicalAWSCloudRuntimeDriftID(write, candidate)
		payload, err := factschema.EncodeReducerAWSCloudRuntimeDriftFinding(
			awsCloudRuntimeDriftTypedPayload(write, candidate, canonicalID),
		)
		if err != nil {
			return AWSCloudRuntimeDriftWriteResult{}, fmt.Errorf("encode aws cloud runtime drift payload: %w", err)
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return AWSCloudRuntimeDriftWriteResult{}, fmt.Errorf("marshal aws cloud runtime drift payload: %w", err)
		}

		rows = append(rows, reducerFactVersionedRow{
			FactID:           awsCloudRuntimeDriftFactID(write, candidate),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         awsCloudRuntimeDriftFactKind,
			StableFactKey:    awsCloudRuntimeDriftStableFactKey(write, candidate),
			SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
			CollectorKind:    reducerFactCollectorKind(write.SourceSystem),
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
		canonicalIDs = append(canonicalIDs, canonicalID)
	}
	// Bounded chunked bulk insert: candidates are upserted in O(N/batchSize)
	// round-trips rather than one ExecContext per candidate.
	if err := reducerBatchInsertVersionedFacts(ctx, w.DB, rows); err != nil {
		return AWSCloudRuntimeDriftWriteResult{}, fmt.Errorf("write aws cloud runtime drift fact: %w", err)
	}

	return AWSCloudRuntimeDriftWriteResult{
		CanonicalIDs:    canonicalIDs,
		CanonicalWrites: len(canonicalIDs),
		EvidenceSummary: fmt.Sprintf("wrote aws cloud runtime drift findings %d", len(canonicalIDs)),
	}, nil
}

func awsCloudRuntimeDriftFactID(write AWSCloudRuntimeDriftWrite, candidate model.Candidate) string {
	return awsCloudRuntimeDriftFactKind + ":" + facts.StableID(
		awsCloudRuntimeDriftFactKind,
		awsCloudRuntimeDriftIdentity(write, candidate),
	)
}

func awsCloudRuntimeDriftStableFactKey(write AWSCloudRuntimeDriftWrite, candidate model.Candidate) string {
	identity := awsCloudRuntimeDriftIdentity(write, candidate)
	return strings.Join([]string{
		"aws_cloud_runtime_drift",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["finding_kind"])),
		strings.TrimSpace(candidate.CorrelationKey),
	}, ":")
}

func canonicalAWSCloudRuntimeDriftID(write AWSCloudRuntimeDriftWrite, candidate model.Candidate) string {
	return "canonical:" + awsCloudRuntimeDriftStableFactKey(write, candidate)
}

func awsCloudRuntimeDriftIdentity(write AWSCloudRuntimeDriftWrite, candidate model.Candidate) map[string]any {
	return map[string]any{
		"scope_id":       strings.TrimSpace(write.ScopeID),
		"generation_id":  strings.TrimSpace(write.GenerationID),
		"candidate_id":   strings.TrimSpace(candidate.ID),
		"arn":            strings.TrimSpace(candidate.CorrelationKey),
		"finding_kind":   awsCloudRuntimeFindingKind(candidate),
		"candidate_kind": strings.TrimSpace(candidate.Kind),
	}
}

func awsCloudRuntimeDriftTypedPayload(
	write AWSCloudRuntimeDriftWrite,
	candidate model.Candidate,
	canonicalID string,
) reducerderivedv1.AWSCloudRuntimeDriftFinding {
	status := awsCloudRuntimeManagementStatus(candidate)
	return reducerderivedv1.AWSCloudRuntimeDriftFinding{
		ReducerDomain:    string(DomainAWSCloudRuntimeDrift),
		IntentID:         write.IntentID,
		ScopeID:          write.ScopeID,
		GenerationID:     write.GenerationID,
		SourceSystem:     write.SourceSystem,
		Cause:            write.Cause,
		CanonicalID:      canonicalID,
		CandidateID:      candidate.ID,
		CandidateKind:    candidate.Kind,
		ARN:              candidate.CorrelationKey,
		FindingKind:      awsCloudRuntimeFindingKind(candidate),
		ManagementStatus: status,
		Confidence:       candidate.Confidence,
		CandidateState:   string(candidate.State),
		MatchedTerraformStateAddress: awsCloudRuntimeEvidenceValue(
			candidate,
			cloudruntime.EvidenceTypeStateResource,
			"resource_address",
		),
		MissingEvidence:     nonNilStrings(awsCloudRuntimeMissingEvidence(candidate, status)),
		WarningFlags:        nonNilStrings(awsCloudRuntimeWarningFlags(candidate, status)),
		RecommendedAction:   awsCloudRuntimeRecommendedAction(status),
		Evidence:            nonNilMapSlice(awsCloudRuntimeDriftEvidencePayload(candidate.Evidence)),
		OrphanedResources:   write.Summary.OrphanedResources,
		UnmanagedResources:  write.Summary.UnmanagedResources,
		AmbiguousResources:  write.Summary.AmbiguousResources,
		UnknownResources:    write.Summary.UnknownResources,
		PublicationFactKind: awsCloudRuntimeDriftFactKind,
		SourceLayers: []string{
			"source_declaration",
			"applied_declaration",
			"observed_resource",
		},
	}
}

func awsCloudRuntimeManagementStatus(candidate model.Candidate) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == cloudruntime.EvidenceTypeManagementStatus &&
			strings.TrimSpace(atom.Value) != "" {
			return strings.TrimSpace(atom.Value)
		}
	}
	switch cloudruntime.FindingKind(awsCloudRuntimeFindingKind(candidate)) {
	case cloudruntime.FindingKindOrphanedCloudResource:
		return cloudruntime.ManagementStatusCloudOnly
	case cloudruntime.FindingKindUnmanagedCloudResource:
		return cloudruntime.ManagementStatusTerraformStateOnly
	case cloudruntime.FindingKindAmbiguousCloudResource:
		return cloudruntime.ManagementStatusAmbiguous
	case cloudruntime.FindingKindUnknownCloudResource:
		return cloudruntime.ManagementStatusUnknown
	default:
		return cloudruntime.ManagementStatusUnknown
	}
}

func awsCloudRuntimeEvidenceValue(candidate model.Candidate, evidenceType string, key string) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType != evidenceType {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(atom.Key), key) && strings.TrimSpace(atom.Value) != "" {
			return strings.TrimSpace(atom.Value)
		}
	}
	return ""
}

func awsCloudRuntimeMissingEvidence(candidate model.Candidate, status string) []string {
	values := awsCloudRuntimeEvidenceValues(candidate, "missing_evidence")
	if len(values) > 0 {
		return values
	}
	switch status {
	case cloudruntime.ManagementStatusCloudOnly:
		return []string{"terraform_state_resource", "terraform_config_resource"}
	case cloudruntime.ManagementStatusTerraformStateOnly:
		return []string{"terraform_config_resource"}
	case cloudruntime.ManagementStatusAmbiguous:
		return []string{"deterministic_owner_evidence"}
	case cloudruntime.ManagementStatusUnknown:
		return []string{"collector_coverage"}
	default:
		return nil
	}
}

func awsCloudRuntimeWarningFlags(candidate model.Candidate, status string) []string {
	values := awsCloudRuntimeEvidenceValues(candidate, "warning_flag")
	if len(values) > 0 {
		return values
	}
	switch status {
	case cloudruntime.ManagementStatusAmbiguous:
		return []string{"ambiguous_ownership"}
	case cloudruntime.ManagementStatusUnknown:
		return []string{"insufficient_coverage"}
	default:
		return nil
	}
}

func awsCloudRuntimeRecommendedAction(status string) string {
	switch status {
	case cloudruntime.ManagementStatusCloudOnly:
		return "triage_owner_and_import_or_retire"
	case cloudruntime.ManagementStatusTerraformStateOnly:
		return "restore_config_or_prepare_import_block"
	case cloudruntime.ManagementStatusAmbiguous:
		return "collect_stronger_evidence_before_import"
	case cloudruntime.ManagementStatusUnknown:
		return "expand_collector_coverage_or_permissions"
	default:
		return "review_evidence"
	}
}

func awsCloudRuntimeEvidenceValues(candidate model.Candidate, key string) []string {
	values := make([]string, 0)
	seen := map[string]struct{}{}
	for _, atom := range candidate.Evidence {
		if !strings.EqualFold(strings.TrimSpace(atom.Key), key) {
			continue
		}
		value := strings.TrimSpace(atom.Value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func awsCloudRuntimeDriftEvidencePayload(evidence []model.EvidenceAtom) []map[string]any {
	if len(evidence) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(evidence))
	for _, atom := range evidence {
		out = append(out, map[string]any{
			"id":            atom.ID,
			"source_system": atom.SourceSystem,
			"evidence_type": atom.EvidenceType,
			"scope_id":      atom.ScopeID,
			"key":           atom.Key,
			"value":         atom.Value,
			"confidence":    atom.Confidence,
		})
	}
	return out
}
