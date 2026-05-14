package query

const (
	impactDefaultListLimit = 50
	impactMaxListLimit     = 200
)

func normalizeImpactListLimit(limit int) int {
	if limit <= 0 {
		return impactDefaultListLimit
	}
	if limit > impactMaxListLimit {
		return impactMaxListLimit
	}
	return limit
}

func trimImpactRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}
