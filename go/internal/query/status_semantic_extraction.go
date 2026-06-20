package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

const semanticExtractionStatusCapability = "semantic_extraction.status"

func (h *StatusHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *StatusHandler) getSemanticExtractionStatus(w http.ResponseWriter, r *http.Request) {
	report := status.BuildReport(status.RawSnapshot{}, status.DefaultOptions())
	if h != nil && h.StatusReader != nil {
		loaded, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
			return
		}
		report = loaded
	}

	semanticStatus := report.SemanticExtraction
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		semanticExtractionStatusToMap(semanticStatus),
		semanticExtractionStatusTruth(h.profile(), semanticStatus),
	)
}

func semanticExtractionStatusToMap(snapshot status.SemanticExtractionStatus) map[string]any {
	statusJSON := statusJSONFromSemanticExtraction(snapshot)
	result := map[string]any{
		"state":                                 statusJSON.State,
		"reason":                                statusJSON.Reason,
		"provider_configured":                   statusJSON.ProviderConfigured,
		"documentation_observations_enabled":    statusJSON.DocumentationObservationsEnabled,
		"code_hints_enabled":                    statusJSON.CodeHintsEnabled,
		"deterministic_paths_affected":          statusJSON.DeterministicPathsAffected,
		"supported_states":                      statusJSON.SupportedStates,
		"supported_provider_profile_states":     statusJSON.SupportedProviderProfileStates,
		"deterministic_documentation_unblocked": !statusJSON.DeterministicPathsAffected,
	}
	if len(statusJSON.ProviderProfiles) > 0 {
		result["provider_profiles"] = statusJSON.ProviderProfiles
	}
	if statusJSON.Queue != nil {
		result["queue"] = statusJSON.Queue
	}
	if statusJSON.Budget != nil {
		result["budget"] = statusJSON.Budget
	}
	if statusJSON.Audit != nil {
		result["audit"] = statusJSON.Audit
	}
	if statusJSON.Detail != "" {
		result["detail"] = statusJSON.Detail
	}
	if statusJSON.UpdatedAt != "" {
		result["updated_at"] = statusJSON.UpdatedAt
	}
	return result
}

func statusJSONFromSemanticExtraction(snapshot status.SemanticExtractionStatus) semanticExtractionStatusView {
	normalized := status.BuildReport(status.RawSnapshot{SemanticExtraction: snapshot}, status.DefaultOptions()).SemanticExtraction
	view := semanticExtractionStatusView{
		State:                            normalized.State,
		Reason:                           normalized.Reason,
		Detail:                           normalized.Detail,
		ProviderConfigured:               normalized.ProviderConfigured,
		DocumentationObservationsEnabled: normalized.DocumentationObservationsEnabled,
		CodeHintsEnabled:                 normalized.CodeHintsEnabled,
		DeterministicPathsAffected:       normalized.DeterministicPathsAffected,
		ProviderProfiles:                 semanticProviderProfilesToMaps(normalized.ProviderProfiles),
		SupportedStates:                  status.SemanticExtractionSupportedStates(),
		SupportedProviderProfileStates:   status.SemanticProviderProfileSupportedStates(),
	}
	if semanticExtractionQueueHasValues(normalized.Queue) {
		view.Queue = semanticExtractionQueueToMap(normalized.Queue)
	}
	if semanticExtractionBudgetHasValues(normalized.Budget) {
		view.Budget = semanticExtractionBudgetToMap(normalized.Budget)
	}
	if semanticExtractionAuditHasValues(normalized.Audit) {
		view.Audit = semanticExtractionAuditToMap(normalized.Audit)
	}
	if !normalized.UpdatedAt.IsZero() {
		view.UpdatedAt = normalized.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return view
}

type semanticExtractionStatusView struct {
	State                            string
	Reason                           string
	Detail                           string
	ProviderConfigured               bool
	DocumentationObservationsEnabled bool
	CodeHintsEnabled                 bool
	DeterministicPathsAffected       bool
	UpdatedAt                        string
	ProviderProfiles                 []map[string]any
	Queue                            map[string]any
	Budget                           map[string]any
	Audit                            map[string]any
	SupportedStates                  []string
	SupportedProviderProfileStates   []string
}

func semanticProviderProfilesToMaps(profiles []status.SemanticProviderProfileStatus) []map[string]any {
	if len(profiles) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		row := map[string]any{
			"profile_id":               profile.ProfileID,
			"provider_kind":            profile.ProviderKind,
			"credential_source_kind":   profile.CredentialSourceKind,
			"credential_configured":    profile.CredentialConfigured,
			"source_classes":           profile.SourceClasses,
			"source_policy_configured": profile.SourcePolicyConfigured,
			"state":                    profile.State,
			"reason":                   profile.Reason,
		}
		if profile.DisplayName != "" {
			row["display_name"] = profile.DisplayName
		}
		if profile.ModelID != "" {
			row["model_id"] = profile.ModelID
		}
		if profile.EmbeddingDimensions > 0 {
			row["embedding_dimensions"] = profile.EmbeddingDimensions
		}
		if profile.EndpointProfileID != "" {
			row["endpoint_profile_id"] = profile.EndpointProfileID
		}
		if !profile.UpdatedAt.IsZero() {
			row["updated_at"] = profile.UpdatedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return rows
}

func semanticExtractionQueueHasValues(snapshot status.SemanticExtractionQueueSnapshot) bool {
	return snapshot.Total > 0 || len(snapshot.StatusCounts) > 0 ||
		len(snapshot.ProviderProfileCounts) > 0
}

func semanticExtractionQueueToMap(snapshot status.SemanticExtractionQueueSnapshot) map[string]any {
	out := map[string]any{
		"total":                snapshot.Total,
		"pending":              snapshot.Pending,
		"claimed":              snapshot.Claimed,
		"retrying":             snapshot.Retrying,
		"succeeded":            snapshot.Succeeded,
		"dead_letter":          snapshot.DeadLetter,
		"skipped":              snapshot.Skipped,
		"no_provider":          snapshot.NoProvider,
		"policy_denied":        snapshot.PolicyDenied,
		"budget_exhausted":     snapshot.BudgetExhausted,
		"unsafe":               snapshot.Unsafe,
		"provider_unavailable": snapshot.ProviderUnavailable,
		"unchanged":            snapshot.Unchanged,
		"stale":                snapshot.Stale,
	}
	if len(snapshot.StatusCounts) > 0 {
		out["status_counts"] = namedCountsToMaps(snapshot.StatusCounts)
	}
	if len(snapshot.SourceClassCounts) > 0 {
		out["source_class_counts"] = namedCountsToMaps(snapshot.SourceClassCounts)
	}
	if len(snapshot.FailureClassCounts) > 0 {
		out["failure_class_counts"] = namedCountsToMaps(snapshot.FailureClassCounts)
	}
	if len(snapshot.ProviderProfileCounts) > 0 {
		out["provider_profile_counts"] = semanticProviderProfileQueueCountsToMaps(snapshot.ProviderProfileCounts)
	}
	if len(snapshot.PolicyDecisionCounts) > 0 {
		out["policy_decision_counts"] = semanticDecisionCountsToMaps(snapshot.PolicyDecisionCounts)
	}
	if len(snapshot.GuardDecisionCounts) > 0 {
		out["guard_decision_counts"] = semanticDecisionCountsToMaps(snapshot.GuardDecisionCounts)
	}
	if !snapshot.UpdatedAt.IsZero() {
		out["updated_at"] = snapshot.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func semanticExtractionBudgetHasValues(snapshot status.SemanticExtractionBudgetSnapshot) bool {
	return snapshot.EstimatedInputTokens > 0 || snapshot.EstimatedOutputTokens > 0 ||
		snapshot.EstimatedCostMicros > 0 || snapshot.ActualInputTokens > 0 ||
		snapshot.ActualOutputTokens > 0 || snapshot.ActualCostMicros > 0 ||
		snapshot.RemainingTokens > 0 || snapshot.RemainingCostMicros > 0 ||
		snapshot.Exhausted > 0 || len(snapshot.DecisionCounts) > 0
}

func semanticExtractionBudgetToMap(snapshot status.SemanticExtractionBudgetSnapshot) map[string]any {
	out := map[string]any{
		"estimated_input_tokens":  snapshot.EstimatedInputTokens,
		"estimated_output_tokens": snapshot.EstimatedOutputTokens,
		"estimated_cost_micros":   snapshot.EstimatedCostMicros,
		"actual_input_tokens":     snapshot.ActualInputTokens,
		"actual_output_tokens":    snapshot.ActualOutputTokens,
		"actual_cost_micros":      snapshot.ActualCostMicros,
		"remaining_tokens":        snapshot.RemainingTokens,
		"remaining_cost_micros":   snapshot.RemainingCostMicros,
		"exhausted":               snapshot.Exhausted,
	}
	if len(snapshot.DecisionCounts) > 0 {
		out["decision_counts"] = semanticBudgetDecisionCountsToMaps(snapshot.DecisionCounts)
	}
	return out
}

func semanticExtractionAuditHasValues(snapshot status.SemanticExtractionAuditSnapshot) bool {
	return len(snapshot.ActorClassCounts) > 0 || len(snapshot.ACLStateCounts) > 0 ||
		!snapshot.LastProcessedAt.IsZero()
}

func semanticExtractionAuditToMap(snapshot status.SemanticExtractionAuditSnapshot) map[string]any {
	out := map[string]any{}
	if len(snapshot.ActorClassCounts) > 0 {
		out["actor_class_counts"] = namedCountsToMaps(snapshot.ActorClassCounts)
	}
	if len(snapshot.ACLStateCounts) > 0 {
		out["acl_state_counts"] = namedCountsToMaps(snapshot.ACLStateCounts)
	}
	if !snapshot.LastProcessedAt.IsZero() {
		out["last_processed_at"] = snapshot.LastProcessedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func namedCountsToMaps(rows []status.NamedCount) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"name": row.Name, "count": row.Count})
	}
	return out
}

func semanticProviderProfileQueueCountsToMaps(
	rows []status.SemanticExtractionProviderProfileQueueCount,
) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"provider_kind":          row.ProviderKind,
			"provider_profile_id":    row.ProviderProfileID,
			"provider_profile_class": row.ProviderProfileClass,
			"count":                  row.Count,
		})
	}
	return out
}

func semanticDecisionCountsToMaps(rows []status.SemanticExtractionDecisionCount) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"state":  row.State,
			"reason": row.Reason,
			"count":  row.Count,
		})
	}
	return out
}

func semanticBudgetDecisionCountsToMaps(
	rows []status.SemanticExtractionBudgetDecisionCount,
) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"state":       row.State,
			"reason":      row.Reason,
			"budget_unit": row.BudgetUnit,
			"count":       row.Count,
		})
	}
	return out
}

func semanticExtractionStatusTruth(profile QueryProfile, snapshot status.SemanticExtractionStatus) *TruthEnvelope {
	view := statusJSONFromSemanticExtraction(snapshot)
	truth := BuildTruthEnvelope(
		profile,
		semanticExtractionStatusCapability,
		TruthBasisHybrid,
		view.Detail,
	)
	if view.State != status.SemanticExtractionAvailable {
		truth.Level = TruthLevelFallback
		truth.Freshness = TruthFreshness{
			State:  FreshnessUnavailable,
			Detail: view.Reason,
		}
	}
	return truth
}
