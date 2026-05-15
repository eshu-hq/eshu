package currentpath

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/semanticeval"
)

var rankedResultKeys = []string{
	"results",
	"matches",
	"evidence_groups",
	"matched_symbols",
	"matched_files",
}

func mapTruthLevel(truth *truthEnvelope) semanticeval.TruthClass {
	if truth == nil {
		return semanticeval.TruthClassDerived
	}
	switch truth.Level {
	case "exact":
		return semanticeval.TruthClassExact
	case "derived", "fallback":
		return semanticeval.TruthClassDerived
	default:
		return semanticeval.TruthClassDerived
	}
}

func extractCandidates(data json.RawMessage, truth semanticeval.TruthClass) []semanticeval.Candidate {
	if len(data) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	for _, key := range rankedResultKeys {
		rows, ok := payload[key].([]any)
		if !ok {
			continue
		}
		return candidatesFromRows(rows, truth)
	}
	return nil
}

func candidatesFromRows(rows []any, truth semanticeval.TruthClass) []semanticeval.Candidate {
	candidates := make([]semanticeval.Candidate, 0, len(rows))
	for _, row := range rows {
		values, ok := row.(map[string]any)
		if !ok {
			continue
		}
		handle := rowHandle(values)
		if handle == "" {
			continue
		}
		candidate := semanticeval.Candidate{Handle: handle, Truth: truth}
		if score, ok := numberValue(values, "score"); ok {
			candidate.Score = score
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func rowHandle(values map[string]any) string {
	for _, key := range []string{"source_handle", "handle", "entity_id"} {
		if value := stringValue(values, key); value != "" {
			return value
		}
	}
	path := firstStringValue(values, "file_path", "relative_path", "path")
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "file://") {
		return path
	}
	repoID := firstStringValue(values, "repo_id", "repo")
	if repoID == "" {
		return "file://" + strings.TrimLeft(path, "/")
	}
	return fmt.Sprintf("file://%s/%s", repoID, strings.TrimLeft(path, "/"))
}

func firstStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(values map[string]any, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func numberValue(values map[string]any, key string) (float64, bool) {
	switch value := values[key].(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	default:
		return 0, false
	}
}
