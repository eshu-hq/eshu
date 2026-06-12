package query

import "strings"

func pruneEmptyRelationshipProvenance(row map[string]any) {
	if len(row) == 0 {
		return
	}
	if raw, ok := row["confidence"]; !ok || raw == nil || floatVal(row, "confidence") == 0 {
		delete(row, "confidence")
	}
	if strings.TrimSpace(StringVal(row, "resolution_method")) == "" {
		delete(row, "resolution_method")
	}
	if strings.TrimSpace(StringVal(row, "reason")) == "" {
		delete(row, "reason")
	}
}
