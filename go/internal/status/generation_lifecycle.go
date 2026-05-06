package status

import (
	"fmt"
	"strings"
	"time"
)

// GenerationTransitionSnapshot captures one recent scope-generation lifecycle
// row straight from the status store.
type GenerationTransitionSnapshot struct {
	ScopeID                   string
	GenerationID              string
	Status                    string
	TriggerKind               string
	FreshnessHint             string
	ObservedAt                time.Time
	ActivatedAt               time.Time
	SupersededAt              time.Time
	CurrentActiveGenerationID string
}

type generationTransitionJSON struct {
	ScopeID                   string `json:"scope_id"`
	GenerationID              string `json:"generation_id"`
	Status                    string `json:"status"`
	TriggerKind               string `json:"trigger_kind"`
	FreshnessHint             string `json:"freshness_hint,omitempty"`
	ObservedAt                string `json:"observed_at"`
	ActivatedAt               string `json:"activated_at,omitempty"`
	SupersededAt              string `json:"superseded_at,omitempty"`
	CurrentActiveGenerationID string `json:"current_active_generation_id,omitempty"`
}

func cloneGenerationTransitions(rows []GenerationTransitionSnapshot) []GenerationTransitionSnapshot {
	if len(rows) == 0 {
		return nil
	}

	cloned := make([]GenerationTransitionSnapshot, len(rows))
	copy(cloned, rows)
	return cloned
}

func generationTransitionsText(rows []GenerationTransitionSnapshot) string {
	if len(rows) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf(
			"scope=%s generation=%s status=%s trigger=%s freshness=%s observed=%s activated=%s superseded=%s current_active=%s",
			transitionField(row.ScopeID),
			transitionField(row.GenerationID),
			transitionField(row.Status),
			transitionField(row.TriggerKind),
			transitionField(row.FreshnessHint),
			transitionTime(row.ObservedAt),
			transitionTime(row.ActivatedAt),
			transitionTime(row.SupersededAt),
			transitionField(row.CurrentActiveGenerationID),
		))
	}

	return strings.Join(parts, "; ")
}

func generationTransitionsJSON(rows []GenerationTransitionSnapshot) []generationTransitionJSON {
	if len(rows) == 0 {
		return nil
	}

	projected := make([]generationTransitionJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, generationTransitionJSONFromReport(row))
	}

	return projected
}

func generationTransitionJSONFromReport(row GenerationTransitionSnapshot) generationTransitionJSON {
	return generationTransitionJSON{
		ScopeID:                   strings.TrimSpace(row.ScopeID),
		GenerationID:              strings.TrimSpace(row.GenerationID),
		Status:                    strings.TrimSpace(row.Status),
		TriggerKind:               strings.TrimSpace(row.TriggerKind),
		FreshnessHint:             transitionField(row.FreshnessHint),
		ObservedAt:                transitionTime(row.ObservedAt),
		ActivatedAt:               transitionTime(row.ActivatedAt),
		SupersededAt:              transitionTime(row.SupersededAt),
		CurrentActiveGenerationID: transitionField(row.CurrentActiveGenerationID),
	}
}

func transitionField(value string) string {
	return strings.TrimSpace(value)
}

func transitionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.UTC().Format(time.RFC3339)
}
