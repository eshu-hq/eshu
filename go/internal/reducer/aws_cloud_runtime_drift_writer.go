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
)

const awsCloudRuntimeDriftFactKind = "reducer_aws_cloud_runtime_drift_finding"

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
	for _, candidate := range write.Candidates {
		canonicalID := canonicalAWSCloudRuntimeDriftID(write, candidate)
		payloadJSON, err := json.Marshal(awsCloudRuntimeDriftPayload(write, candidate, canonicalID))
		if err != nil {
			return AWSCloudRuntimeDriftWriteResult{}, fmt.Errorf("marshal aws cloud runtime drift payload: %w", err)
		}

		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			awsCloudRuntimeDriftFactID(write, candidate),
			write.ScopeID,
			write.GenerationID,
			awsCloudRuntimeDriftFactKind,
			awsCloudRuntimeDriftStableFactKey(write, candidate),
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
			return AWSCloudRuntimeDriftWriteResult{}, fmt.Errorf("write aws cloud runtime drift fact: %w", err)
		}
		canonicalIDs = append(canonicalIDs, canonicalID)
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

func awsCloudRuntimeDriftPayload(
	write AWSCloudRuntimeDriftWrite,
	candidate model.Candidate,
	canonicalID string,
) map[string]any {
	status := awsCloudRuntimeManagementStatus(candidate)
	return map[string]any{
		"reducer_domain":    string(DomainAWSCloudRuntimeDrift),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"canonical_id":      canonicalID,
		"candidate_id":      candidate.ID,
		"candidate_kind":    candidate.Kind,
		"arn":               candidate.CorrelationKey,
		"finding_kind":      awsCloudRuntimeFindingKind(candidate),
		"management_status": status,
		"confidence":        candidate.Confidence,
		"candidate_state":   string(candidate.State),
		"matched_terraform_state_address": awsCloudRuntimeEvidenceValue(
			candidate,
			cloudruntime.EvidenceTypeStateResource,
			"resource_address",
		),
		"missing_evidence":      awsCloudRuntimeMissingEvidence(candidate, status),
		"warning_flags":         awsCloudRuntimeWarningFlags(candidate, status),
		"recommended_action":    awsCloudRuntimeRecommendedAction(status),
		"evidence":              awsCloudRuntimeDriftEvidencePayload(candidate.Evidence),
		"orphaned_resources":    write.Summary.OrphanedResources,
		"unmanaged_resources":   write.Summary.UnmanagedResources,
		"ambiguous_resources":   write.Summary.AmbiguousResources,
		"unknown_resources":     write.Summary.UnknownResources,
		"publication_fact_kind": awsCloudRuntimeDriftFactKind,
		"source_layers": []string{
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
