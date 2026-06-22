package catalog

// plannerExcludedSurfaces is the curated set of implemented surfaces that must
// never appear in Ask Eshu's answer-planning catalog. Most entries are
// side-effecting admin/recovery routes; browser-session routes are session
// control surfaces that either mutate server-side session state or expose only
// caller-local auth metadata, so they are not retrieval paths for answering
// repository, graph, runtime, or cloud questions.
//
// Completeness is enforced by tests: every name here must be an implemented
// api_route in the inventory (no stale entries), every name here must be absent
// from the parsed catalog, and every implemented surface must be either a
// catalog entry or listed here (nothing silently vanishes).
func plannerExcludedSurfaces() map[string]struct{} {
	return map[string]struct{}{
		"DELETE /api/v0/auth/browser-session":        {}, // revokes the caller's browser session
		"GET /api/v0/auth/browser-session":           {}, // reads caller-local session metadata only
		"PATCH /api/v0/auth/browser-session/context": {}, // switches the caller's tenant/workspace context
		"POST /api/v0/admin/backfill":                {}, // RequestBackfill enqueues backfill work
		"POST /api/v0/admin/dead-letter":             {}, // DeadLetterWorkItems dead-letters queued work
		"POST /api/v0/admin/refinalize":              {}, // re-enqueues projector work for the given scope
		"POST /api/v0/admin/reindex":                 {}, // RequestReindex enqueues ingester reindex work
		"POST /api/v0/admin/replay":                  {}, // ReplayFailed re-processes failed work items
		"POST /api/v0/admin/skip":                    {}, // skips queued work items
		"POST /api/v0/auth/browser-session":          {}, // creates a caller browser session and cookies
	}
}

// isPlannerExcludedSurface reports whether a surface name is deliberately kept
// out of Ask Eshu's retrieval catalog.
func isPlannerExcludedSurface(name string) bool {
	_, ok := plannerExcludedSurfaces()[name]
	return ok
}
