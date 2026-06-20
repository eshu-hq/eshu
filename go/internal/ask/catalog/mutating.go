package catalog

// mutatingSurfaces is the curated set of implemented surfaces that perform a
// side effect rather than a read. Ask Eshu is read-only end to end, so these
// surfaces must never appear in the planner catalog: Parse excludes any surface
// whose name is listed here, making the catalog read-only by construction. The
// planner therefore cannot select a write or recovery action as a retrieval
// path.
//
// Every entry is a state-changing admin/recovery route confirmed by reading its
// handler in go/internal/query/admin.go (each enqueues, re-enqueues, replays,
// backfills, dead-letters, or skips queued work). The read counterparts under
// /api/v0/admin/*/query are NOT listed here; they stay in the catalog as normal
// Postgres reads.
//
// Completeness is enforced by tests: every name here must be an implemented
// api_route in the inventory (no stale entries), every name here must be absent
// from the parsed catalog, and every implemented surface must be either a
// catalog entry or listed here (nothing silently vanishes).
func mutatingSurfaces() map[string]struct{} {
	return map[string]struct{}{
		"POST /api/v0/admin/backfill":    {}, // RequestBackfill enqueues backfill work
		"POST /api/v0/admin/dead-letter": {}, // DeadLetterWorkItems dead-letters queued work
		"POST /api/v0/admin/refinalize":  {}, // re-enqueues projector work for the given scope
		"POST /api/v0/admin/reindex":     {}, // RequestReindex enqueues ingester reindex work
		"POST /api/v0/admin/replay":      {}, // ReplayFailed re-processes failed work items
		"POST /api/v0/admin/skip":        {}, // skips queued work items
	}
}

// isMutatingSurface reports whether a surface name is a curated side-effecting
// surface that must be excluded from the read-only planner catalog.
func isMutatingSurface(name string) bool {
	_, ok := mutatingSurfaces()[name]
	return ok
}
