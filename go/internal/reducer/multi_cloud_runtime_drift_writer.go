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
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const multiCloudRuntimeDriftFactKind = "reducer_multi_cloud_runtime_drift_finding"

// PostgresMultiCloudRuntimeDriftWriter persists admitted provider-neutral runtime
// drift findings into the shared fact store.
type PostgresMultiCloudRuntimeDriftWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteMultiCloudRuntimeDriftFindings stores one durable fact per admitted
// finding. The fact id is stable by candidate identity (scope, generation,
// finding kind, canonical uid) so reducer retries and replays upsert the same row
// instead of duplicating findings under concurrent workers.
func (w PostgresMultiCloudRuntimeDriftWriter) WriteMultiCloudRuntimeDriftFindings(
	ctx context.Context,
	write MultiCloudRuntimeDriftWrite,
) (MultiCloudRuntimeDriftWriteResult, error) {
	if w.DB == nil {
		return MultiCloudRuntimeDriftWriteResult{}, fmt.Errorf("multi cloud runtime drift database is required")
	}

	now := reducerWriterNow(w.Now)
	canonicalIDs := make([]string, 0, len(write.Candidates))
	for _, candidate := range write.Candidates {
		canonicalID := canonicalMultiCloudRuntimeDriftID(write, candidate)
		payloadJSON, err := json.Marshal(multiCloudRuntimeDriftPayload(write, candidate, canonicalID))
		if err != nil {
			return MultiCloudRuntimeDriftWriteResult{}, fmt.Errorf("marshal multi cloud runtime drift payload: %w", err)
		}

		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			multiCloudRuntimeDriftFactID(write, candidate),
			write.ScopeID,
			write.GenerationID,
			multiCloudRuntimeDriftFactKind,
			multiCloudRuntimeDriftStableFactKey(write, candidate),
			reducerFactCollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			write.SourceSystem,
			write.IntentID,
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return MultiCloudRuntimeDriftWriteResult{}, fmt.Errorf("write multi cloud runtime drift fact: %w", err)
		}
		canonicalIDs = append(canonicalIDs, canonicalID)
	}

	return MultiCloudRuntimeDriftWriteResult{
		CanonicalIDs:    canonicalIDs,
		CanonicalWrites: len(canonicalIDs),
		EvidenceSummary: fmt.Sprintf("wrote multi cloud runtime drift findings %d", len(canonicalIDs)),
	}, nil
}

func multiCloudRuntimeDriftFactID(write MultiCloudRuntimeDriftWrite, candidate model.Candidate) string {
	return multiCloudRuntimeDriftFactKind + ":" + facts.StableID(
		multiCloudRuntimeDriftFactKind,
		multiCloudRuntimeDriftIdentity(write, candidate),
	)
}

func multiCloudRuntimeDriftStableFactKey(write MultiCloudRuntimeDriftWrite, candidate model.Candidate) string {
	identity := multiCloudRuntimeDriftIdentity(write, candidate)
	return strings.Join([]string{
		"multi_cloud_runtime_drift",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["finding_kind"])),
		strings.TrimSpace(candidate.CorrelationKey),
	}, ":")
}

func canonicalMultiCloudRuntimeDriftID(write MultiCloudRuntimeDriftWrite, candidate model.Candidate) string {
	return "canonical:" + multiCloudRuntimeDriftStableFactKey(write, candidate)
}

func multiCloudRuntimeDriftIdentity(write MultiCloudRuntimeDriftWrite, candidate model.Candidate) map[string]any {
	return map[string]any{
		"scope_id":           strings.TrimSpace(write.ScopeID),
		"generation_id":      strings.TrimSpace(write.GenerationID),
		"candidate_id":       strings.TrimSpace(candidate.ID),
		"cloud_resource_uid": strings.TrimSpace(candidate.CorrelationKey),
		"finding_kind":       multiCloudRuntimeFindingKind(candidate),
		"candidate_kind":     strings.TrimSpace(candidate.Kind),
	}
}

func multiCloudRuntimeDriftPayload(
	write MultiCloudRuntimeDriftWrite,
	candidate model.Candidate,
	canonicalID string,
) map[string]any {
	status := multicloud.ManagementStatusFromCandidate(candidate)
	return map[string]any{
		"reducer_domain":     string(DomainMultiCloudRuntimeDrift),
		"intent_id":          write.IntentID,
		"scope_id":           write.ScopeID,
		"generation_id":      write.GenerationID,
		"source_system":      write.SourceSystem,
		"cause":              write.Cause,
		"canonical_id":       canonicalID,
		"candidate_id":       candidate.ID,
		"candidate_kind":     candidate.Kind,
		"cloud_resource_uid": candidate.CorrelationKey,
		"provider":           multicloud.ProviderFromCandidate(candidate),
		"raw_identity":       multiCloudRuntimeRawIdentity(candidate),
		"finding_kind":       multiCloudRuntimeFindingKind(candidate),
		"management_status":  status,
		"confidence":         candidate.Confidence,
		"candidate_state":    string(candidate.State),
		"matched_terraform_state_address": multiCloudRuntimeEvidenceValue(
			candidate,
			multicloud.EvidenceTypeStateResource,
			"resource_address",
		),
		"missing_evidence":      multiCloudRuntimeMissingEvidence(candidate, status),
		"warning_flags":         multiCloudRuntimeWarningFlags(candidate, status),
		"recommended_action":    multiCloudRuntimeRecommendedAction(status),
		"evidence":              multiCloudRuntimeDriftEvidencePayload(candidate.Evidence),
		"orphaned_resources":    write.Summary.OrphanedResources,
		"unmanaged_resources":   write.Summary.UnmanagedResources,
		"ambiguous_resources":   write.Summary.AmbiguousResources,
		"unknown_resources":     write.Summary.UnknownResources,
		"publication_fact_kind": multiCloudRuntimeDriftFactKind,
		"source_layers": []string{
			"source_declaration",
			"applied_declaration",
			"observed_resource",
		},
	}
}

func multiCloudRuntimeRawIdentity(candidate model.Candidate) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == multicloud.EvidenceTypeRawIdentity {
			return strings.TrimSpace(atom.Value)
		}
	}
	return ""
}

func multiCloudRuntimeEvidenceValue(candidate model.Candidate, evidenceType string, key string) string {
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

func multiCloudRuntimeMissingEvidence(candidate model.Candidate, status string) []string {
	values := multiCloudRuntimeEvidenceValues(candidate, "missing_evidence")
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

func multiCloudRuntimeWarningFlags(candidate model.Candidate, status string) []string {
	values := multiCloudRuntimeEvidenceValues(candidate, "warning_flag")
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

func multiCloudRuntimeRecommendedAction(status string) string {
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

func multiCloudRuntimeEvidenceValues(candidate model.Candidate, key string) []string {
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

func multiCloudRuntimeDriftEvidencePayload(evidence []model.EvidenceAtom) []map[string]any {
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
