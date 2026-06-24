// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// retryPoliciesToSlice converts []RetryPolicySummary to a slice of maps.
func retryPoliciesToSlice(policies []status.RetryPolicySummary) []map[string]any {
	if len(policies) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(policies))
	for _, p := range policies {
		result = append(result, map[string]any{
			"stage":               p.Stage,
			"max_attempts":        p.MaxAttempts,
			"retry_delay":         p.RetryDelay.String(),
			"retry_delay_ms":      p.RetryDelay.Milliseconds(),
			"retry_delay_seconds": p.RetryDelay.Seconds(),
		})
	}
	return result
}

// generationTransitionsToSlice converts []GenerationTransitionSnapshot to a slice of maps.
func generationTransitionsToSlice(transitions []status.GenerationTransitionSnapshot) []map[string]any {
	if len(transitions) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(transitions))
	for _, t := range transitions {
		item := map[string]any{
			"scope_id":      t.ScopeID,
			"generation_id": t.GenerationID,
			"status":        t.Status,
			"trigger_kind":  t.TriggerKind,
		}

		if t.FreshnessHint != "" {
			item["freshness_hint"] = t.FreshnessHint
		}
		if !t.ObservedAt.IsZero() {
			item["observed_at"] = t.ObservedAt.Format(time.RFC3339)
		}
		if !t.ActivatedAt.IsZero() {
			item["activated_at"] = t.ActivatedAt.Format(time.RFC3339)
		}
		if !t.SupersededAt.IsZero() {
			item["superseded_at"] = t.SupersededAt.Format(time.RFC3339)
		}
		if t.CurrentActiveGenerationID != "" {
			item["current_active_generation_id"] = t.CurrentActiveGenerationID
		}

		result = append(result, item)
	}
	return result
}
