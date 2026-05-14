package query

const (
	resolveEntityDefaultLimit = 10
	resolveEntityMaxLimit     = 100
)

func normalizeResolveEntityLimit(limit int) int {
	if limit <= 0 {
		return resolveEntityDefaultLimit
	}
	if limit > resolveEntityMaxLimit {
		return resolveEntityMaxLimit
	}
	return limit
}

func resolvedEntityResponse(entities []map[string]any, limit int, truncated bool) map[string]any {
	return map[string]any{
		"entities":  entities,
		"matches":   entities,
		"count":     len(entities),
		"limit":     limit,
		"truncated": truncated,
	}
}

func trimResolvedEntityPage(entities []map[string]any, limit int) ([]map[string]any, bool) {
	if len(entities) <= limit {
		return entities, false
	}
	return entities[:limit], true
}
