// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "time"

type semanticExtractionQueueJSON struct {
	Total                 int                                          `json:"total"`
	Pending               int                                          `json:"pending"`
	Claimed               int                                          `json:"claimed"`
	Retrying              int                                          `json:"retrying"`
	Succeeded             int                                          `json:"succeeded"`
	DeadLetter            int                                          `json:"dead_letter"`
	Skipped               int                                          `json:"skipped"`
	NoProvider            int                                          `json:"no_provider"`
	PolicyDenied          int                                          `json:"policy_denied"`
	BudgetExhausted       int                                          `json:"budget_exhausted"`
	Unsafe                int                                          `json:"unsafe"`
	ProviderUnavailable   int                                          `json:"provider_unavailable"`
	Unchanged             int                                          `json:"unchanged"`
	Stale                 int                                          `json:"stale"`
	StatusCounts          []namedCountJSON                             `json:"status_counts,omitempty"`
	SourceClassCounts     []namedCountJSON                             `json:"source_class_counts,omitempty"`
	FailureClassCounts    []namedCountJSON                             `json:"failure_class_counts,omitempty"`
	ProviderProfileCounts []semanticExtractionProviderProfileQueueJSON `json:"provider_profile_counts,omitempty"`
	PolicyDecisionCounts  []semanticExtractionDecisionJSON             `json:"policy_decision_counts,omitempty"`
	GuardDecisionCounts   []semanticExtractionDecisionJSON             `json:"guard_decision_counts,omitempty"`
	UpdatedAt             string                                       `json:"updated_at,omitempty"`
}

type semanticExtractionProviderProfileQueueJSON struct {
	ProviderKind         string `json:"provider_kind,omitempty"`
	ProviderProfileID    string `json:"provider_profile_id,omitempty"`
	ProviderProfileClass string `json:"provider_profile_class,omitempty"`
	Count                int    `json:"count"`
}

type semanticExtractionDecisionJSON struct {
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Count  int    `json:"count"`
}

type semanticExtractionBudgetJSON struct {
	EstimatedInputTokens  int64                                  `json:"estimated_input_tokens"`
	EstimatedOutputTokens int64                                  `json:"estimated_output_tokens"`
	EstimatedCostMicros   int64                                  `json:"estimated_cost_micros"`
	ActualInputTokens     int64                                  `json:"actual_input_tokens"`
	ActualOutputTokens    int64                                  `json:"actual_output_tokens"`
	ActualCostMicros      int64                                  `json:"actual_cost_micros"`
	RemainingTokens       int64                                  `json:"remaining_tokens"`
	RemainingCostMicros   int64                                  `json:"remaining_cost_micros"`
	Exhausted             int                                    `json:"exhausted"`
	DecisionCounts        []semanticExtractionBudgetDecisionJSON `json:"decision_counts,omitempty"`
}

type semanticExtractionBudgetDecisionJSON struct {
	State      string `json:"state,omitempty"`
	Reason     string `json:"reason,omitempty"`
	BudgetUnit string `json:"budget_unit,omitempty"`
	Count      int    `json:"count"`
}

type semanticExtractionAuditJSON struct {
	ActorClassCounts []namedCountJSON `json:"actor_class_counts,omitempty"`
	ACLStateCounts   []namedCountJSON `json:"acl_state_counts,omitempty"`
	LastProcessedAt  string           `json:"last_processed_at,omitempty"`
}

func semanticExtractionQueueStatusJSON(snapshot SemanticExtractionQueueSnapshot) *semanticExtractionQueueJSON {
	out := &semanticExtractionQueueJSON{
		Total:                 snapshot.Total,
		Pending:               snapshot.Pending,
		Claimed:               snapshot.Claimed,
		Retrying:              snapshot.Retrying,
		Succeeded:             snapshot.Succeeded,
		DeadLetter:            snapshot.DeadLetter,
		Skipped:               snapshot.Skipped,
		NoProvider:            snapshot.NoProvider,
		PolicyDenied:          snapshot.PolicyDenied,
		BudgetExhausted:       snapshot.BudgetExhausted,
		Unsafe:                snapshot.Unsafe,
		ProviderUnavailable:   snapshot.ProviderUnavailable,
		Unchanged:             snapshot.Unchanged,
		Stale:                 snapshot.Stale,
		StatusCounts:          namedCountsJSON(snapshot.StatusCounts),
		SourceClassCounts:     namedCountsJSON(snapshot.SourceClassCounts),
		FailureClassCounts:    namedCountsJSON(snapshot.FailureClassCounts),
		ProviderProfileCounts: semanticExtractionProviderProfileQueueCountsJSON(snapshot.ProviderProfileCounts),
		PolicyDecisionCounts:  semanticExtractionDecisionCountsJSON(snapshot.PolicyDecisionCounts),
		GuardDecisionCounts:   semanticExtractionDecisionCountsJSON(snapshot.GuardDecisionCounts),
	}
	if !snapshot.UpdatedAt.IsZero() {
		out.UpdatedAt = snapshot.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func semanticExtractionProviderProfileQueueCountsJSON(
	rows []SemanticExtractionProviderProfileQueueCount,
) []semanticExtractionProviderProfileQueueJSON {
	if len(rows) == 0 {
		return nil
	}
	out := make([]semanticExtractionProviderProfileQueueJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, semanticExtractionProviderProfileQueueJSON(row))
	}
	return out
}

func semanticExtractionDecisionCountsJSON(rows []SemanticExtractionDecisionCount) []semanticExtractionDecisionJSON {
	if len(rows) == 0 {
		return nil
	}
	out := make([]semanticExtractionDecisionJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, semanticExtractionDecisionJSON(row))
	}
	return out
}

func semanticExtractionBudgetStatusJSON(snapshot SemanticExtractionBudgetSnapshot) *semanticExtractionBudgetJSON {
	return &semanticExtractionBudgetJSON{
		EstimatedInputTokens:  snapshot.EstimatedInputTokens,
		EstimatedOutputTokens: snapshot.EstimatedOutputTokens,
		EstimatedCostMicros:   snapshot.EstimatedCostMicros,
		ActualInputTokens:     snapshot.ActualInputTokens,
		ActualOutputTokens:    snapshot.ActualOutputTokens,
		ActualCostMicros:      snapshot.ActualCostMicros,
		RemainingTokens:       snapshot.RemainingTokens,
		RemainingCostMicros:   snapshot.RemainingCostMicros,
		Exhausted:             snapshot.Exhausted,
		DecisionCounts:        semanticExtractionBudgetDecisionCountsJSON(snapshot.DecisionCounts),
	}
}

func semanticExtractionBudgetDecisionCountsJSON(
	rows []SemanticExtractionBudgetDecisionCount,
) []semanticExtractionBudgetDecisionJSON {
	if len(rows) == 0 {
		return nil
	}
	out := make([]semanticExtractionBudgetDecisionJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, semanticExtractionBudgetDecisionJSON(row))
	}
	return out
}

func semanticExtractionAuditStatusJSON(snapshot SemanticExtractionAuditSnapshot) *semanticExtractionAuditJSON {
	out := &semanticExtractionAuditJSON{
		ActorClassCounts: namedCountsJSON(snapshot.ActorClassCounts),
		ACLStateCounts:   namedCountsJSON(snapshot.ACLStateCounts),
	}
	if !snapshot.LastProcessedAt.IsZero() {
		out.LastProcessedAt = snapshot.LastProcessedAt.UTC().Format(time.RFC3339)
	}
	return out
}
