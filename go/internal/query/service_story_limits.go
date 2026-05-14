package query

import "fmt"

func boundedServiceStoryRawValue(value any) (any, map[string]any) {
	switch typed := value.(type) {
	case []map[string]any:
		capped, truncated := capMapRows(typed, serviceStoryItemLimit)
		return capped, serviceStoryRawLimit(len(typed), truncated)
	case map[string]any:
		return boundedServiceStoryRawMap(typed)
	default:
		return value, nil
	}
}

func boundedServiceStoryRawMap(input map[string]any) (map[string]any, map[string]any) {
	out := copyMap(input)
	limits := map[string]any{}
	for _, key := range []string{"artifacts", "delivery_paths", "delivery_workflows", "shared_config_paths"} {
		rows := mapSliceValue(input, key)
		if len(rows) == 0 {
			continue
		}
		capped, truncated := capMapRows(rows, serviceStoryItemLimit)
		out[key] = capped
		limits[key] = serviceStoryRawLimit(len(rows), truncated)
	}
	if len(limits) > 0 {
		out["raw_limits"] = limits
	}
	return out, limits
}

func serviceStoryRawLimit(count int, truncated bool) map[string]any {
	return map[string]any{
		"count":     count,
		"limit":     serviceStoryItemLimit,
		"truncated": truncated,
	}
}

func serviceRelationshipKey(row map[string]any) string {
	if resolvedID := StringVal(row, "resolved_id"); resolvedID != "" {
		return "resolved:" + resolvedID
	}
	return fmt.Sprintf(
		"%s|%s|%s|%s",
		StringVal(row, "relationship_type"),
		StringVal(row, "source_repo_id"),
		StringVal(row, "target_repo_id"),
		StringVal(row, "target_id"),
	)
}

func mergeServiceRelationshipRow(existing map[string]any, incoming map[string]any) {
	if confidence := relationshipFloatVal(incoming, "confidence"); confidence > relationshipFloatVal(existing, "confidence") {
		existing["confidence"] = confidence
	}
	if evidenceCount := firstPositiveInt(incoming, "evidence_count"); evidenceCount > firstPositiveInt(existing, "evidence_count") {
		existing["evidence_count"] = evidenceCount
	}
	if StringVal(existing, "rationale") == "" && StringVal(incoming, "rationale") != "" {
		existing["rationale"] = StringVal(incoming, "rationale")
	}
}
