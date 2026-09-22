// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

type extractionQueueJSON struct {
	Total                 int                                  `json:"total"`
	Pending               int                                  `json:"pending"`
	Claimed               int                                  `json:"claimed"`
	Retrying              int                                  `json:"retrying"`
	Succeeded             int                                  `json:"succeeded"`
	DeadLetter            int                                  `json:"dead_letter"`
	Skipped               int                                  `json:"skipped"`
	NoProvider            int                                  `json:"no_provider"`
	PolicyDenied          int                                  `json:"policy_denied"`
	BudgetExhausted       int                                  `json:"budget_exhausted"`
	Unsafe                int                                  `json:"unsafe"`
	ProviderUnavailable   int                                  `json:"provider_unavailable"`
	Unchanged             int                                  `json:"unchanged"`
	Stale                 int                                  `json:"stale"`
	StatusCounts          []shared.NamedCountJSON              `json:"status_counts,omitempty"`
	SourceClassCounts     []shared.NamedCountJSON              `json:"source_class_counts,omitempty"`
	FailureClassCounts    []shared.NamedCountJSON              `json:"failure_class_counts,omitempty"`
	ProviderProfileCounts []extractionProviderProfileQueueJSON `json:"provider_profile_counts,omitempty"`
	PolicyDecisionCounts  []extractionDecisionJSON             `json:"policy_decision_counts,omitempty"`
	GuardDecisionCounts   []extractionDecisionJSON             `json:"guard_decision_counts,omitempty"`
	UpdatedAt             string                               `json:"updated_at,omitempty"`
}

type extractionProviderProfileQueueJSON struct {
	ProviderKind         string `json:"provider_kind,omitempty"`
	ProviderProfileID    string `json:"provider_profile_id,omitempty"`
	ProviderProfileClass string `json:"provider_profile_class,omitempty"`
	Count                int    `json:"count"`
}

type extractionDecisionJSON struct {
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Count  int    `json:"count"`
}

type extractionBudgetJSON struct {
	EstimatedInputTokens  int64                          `json:"estimated_input_tokens"`
	EstimatedOutputTokens int64                          `json:"estimated_output_tokens"`
	EstimatedCostMicros   int64                          `json:"estimated_cost_micros"`
	ActualInputTokens     int64                          `json:"actual_input_tokens"`
	ActualOutputTokens    int64                          `json:"actual_output_tokens"`
	ActualCostMicros      int64                          `json:"actual_cost_micros"`
	RemainingTokens       int64                          `json:"remaining_tokens"`
	RemainingCostMicros   int64                          `json:"remaining_cost_micros"`
	Exhausted             int                            `json:"exhausted"`
	DecisionCounts        []extractionBudgetDecisionJSON `json:"decision_counts,omitempty"`
}

type extractionBudgetDecisionJSON struct {
	State      string `json:"state,omitempty"`
	Reason     string `json:"reason,omitempty"`
	BudgetUnit string `json:"budget_unit,omitempty"`
	Count      int    `json:"count"`
}

type extractionAuditJSON struct {
	ActorClassCounts []shared.NamedCountJSON `json:"actor_class_counts,omitempty"`
	ACLStateCounts   []shared.NamedCountJSON `json:"acl_state_counts,omitempty"`
	LastProcessedAt  string                  `json:"last_processed_at,omitempty"`
}

func extractionQueueStatusJSON(snapshot ExtractionQueueSnapshot) *extractionQueueJSON {
	out := &extractionQueueJSON{
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
		StatusCounts:          shared.NamedCountsJSON(snapshot.StatusCounts),
		SourceClassCounts:     shared.NamedCountsJSON(snapshot.SourceClassCounts),
		FailureClassCounts:    shared.NamedCountsJSON(snapshot.FailureClassCounts),
		ProviderProfileCounts: extractionProviderProfileQueueCountsJSON(snapshot.ProviderProfileCounts),
		PolicyDecisionCounts:  extractionDecisionCountsJSON(snapshot.PolicyDecisionCounts),
		GuardDecisionCounts:   extractionDecisionCountsJSON(snapshot.GuardDecisionCounts),
	}
	if !snapshot.UpdatedAt.IsZero() {
		out.UpdatedAt = snapshot.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func extractionProviderProfileQueueCountsJSON(
	rows []ExtractionProviderProfileQueueCount,
) []extractionProviderProfileQueueJSON {
	if len(rows) == 0 {
		return nil
	}
	out := make([]extractionProviderProfileQueueJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, extractionProviderProfileQueueJSON(row))
	}
	return out
}

func extractionDecisionCountsJSON(rows []ExtractionDecisionCount) []extractionDecisionJSON {
	if len(rows) == 0 {
		return nil
	}
	out := make([]extractionDecisionJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, extractionDecisionJSON(row))
	}
	return out
}

func extractionBudgetStatusJSON(snapshot ExtractionBudgetSnapshot) *extractionBudgetJSON {
	return &extractionBudgetJSON{
		EstimatedInputTokens:  snapshot.EstimatedInputTokens,
		EstimatedOutputTokens: snapshot.EstimatedOutputTokens,
		EstimatedCostMicros:   snapshot.EstimatedCostMicros,
		ActualInputTokens:     snapshot.ActualInputTokens,
		ActualOutputTokens:    snapshot.ActualOutputTokens,
		ActualCostMicros:      snapshot.ActualCostMicros,
		RemainingTokens:       snapshot.RemainingTokens,
		RemainingCostMicros:   snapshot.RemainingCostMicros,
		Exhausted:             snapshot.Exhausted,
		DecisionCounts:        extractionBudgetDecisionCountsJSON(snapshot.DecisionCounts),
	}
}

func extractionBudgetDecisionCountsJSON(
	rows []ExtractionBudgetDecisionCount,
) []extractionBudgetDecisionJSON {
	if len(rows) == 0 {
		return nil
	}
	out := make([]extractionBudgetDecisionJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, extractionBudgetDecisionJSON(row))
	}
	return out
}

func extractionAuditStatusJSON(snapshot ExtractionAuditSnapshot) *extractionAuditJSON {
	out := &extractionAuditJSON{
		ActorClassCounts: shared.NamedCountsJSON(snapshot.ActorClassCounts),
		ACLStateCounts:   shared.NamedCountsJSON(snapshot.ACLStateCounts),
	}
	if !snapshot.LastProcessedAt.IsZero() {
		out.LastProcessedAt = snapshot.LastProcessedAt.UTC().Format(time.RFC3339)
	}
	return out
}
