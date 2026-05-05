package status

import (
	"fmt"
	"strings"
)

// GenerationHistorySnapshot captures low-cardinality generation state for operators.
type GenerationHistorySnapshot struct {
	Active     int
	Pending    int
	Completed  int
	Superseded int
	Failed     int
	Other      int
}

func deriveGenerationHistory(generationTotals map[string]int) GenerationHistorySnapshot {
	history := GenerationHistorySnapshot{
		Active:     generationTotals["active"],
		Pending:    generationTotals["pending"],
		Completed:  generationTotals["completed"],
		Superseded: generationTotals["superseded"],
		Failed:     generationTotals["failed"],
	}

	known := map[string]struct{}{
		"active":     {},
		"pending":    {},
		"completed":  {},
		"superseded": {},
		"failed":     {},
	}
	for name, count := range generationTotals {
		if _, ok := known[name]; ok {
			continue
		}
		history.Other += count
	}

	return history
}

func generationHistoryText(history GenerationHistorySnapshot) string {
	return fmt.Sprintf(
		"active=%d pending=%d completed=%d superseded=%d failed=%d other=%d",
		history.Active,
		history.Pending,
		history.Completed,
		history.Superseded,
		history.Failed,
		history.Other,
	)
}

func generationHistoryJSONFromReport(history GenerationHistorySnapshot) generationHistoryJSON {
	return generationHistoryJSON(history)
}

func scopeUnchangedCount(active, changed int) int {
	if active <= changed {
		return 0
	}
	return active - changed
}

func generationHistoryIsZero(history GenerationHistorySnapshot) bool {
	return history == GenerationHistorySnapshot{}
}

func scopeActivityText(activity ScopeActivitySnapshot) string {
	parts := []string{
		fmt.Sprintf("active=%d", activity.Active),
		fmt.Sprintf("changed=%d", activity.Changed),
		fmt.Sprintf("unchanged=%d", activity.Unchanged),
	}
	return strings.Join(parts, " ")
}
