package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// scopedFreshnessCausalityRoute reports whether the request targets the
// freshness causality read model. The handler redacts raw scope/generation
// identifiers in transitions for scoped tokens, so the route is tenant-filter
// safe.
func scopedFreshnessCausalityRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/freshness-causality"
}

// getFreshnessCausality returns the freshness causality read model: why answers
// are stale (by closed cause), the generation lifecycle including retired
// generations, and pending projection work. It loads one status snapshot (the
// same read path as /api/v0/status/pipeline) and projects it in memory, adding
// no database cost. Scoped tokens receive the same aggregate counts and cause
// observations with raw scope/generation identifiers withheld from transitions.
func (h *StatusHandler) getFreshnessCausality(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	raw, report, err := loadStatusReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	fc := freshnessCausalityFromRawAndReport(raw, report)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		freshnessCausalityToMap(fc, report.AsOf, scopedAuthContext(r.Context())),
		freshnessCausalityTruth(h.profile(), fc, report.AsOf),
	)
}

func freshnessCausalityToMap(fc FreshnessCausality, asOf time.Time, scoped bool) map[string]any {
	return map[string]any{
		"version":            buildinfo.AppVersion(),
		"as_of":              asOf.Format(time.RFC3339),
		"scoped":             scoped,
		"state":              fc.State,
		"causes":             freshnessCausesToSlice(fc.Causes),
		"generations":        freshnessGenerationsToMap(fc.Generations),
		"pending_projection": freshnessPendingProjectionToMap(fc.PendingProjection),
		"recent_transitions": freshnessTransitionsToSlice(fc.RecentTransitions, scoped),
	}
}

func freshnessCausesToSlice(causes []FreshnessCauseStatus) []map[string]any {
	result := make([]map[string]any, 0, len(causes))
	for _, c := range causes {
		result = append(result, map[string]any{
			"cause":         string(c.Cause),
			"observed":      c.Observed,
			"observability": c.Observability,
			"detail":        c.Detail,
			"next_check":    c.NextCheck.asRecommendedNextCall(),
		})
	}
	return result
}

func freshnessGenerationsToMap(g FreshnessGenerations) map[string]any {
	return map[string]any{
		"active":     g.Active,
		"pending":    g.Pending,
		"completed":  g.Completed,
		"superseded": g.Superseded,
		"failed":     g.Failed,
	}
}

func freshnessPendingProjectionToMap(p FreshnessPendingProjection) map[string]any {
	return map[string]any{
		"outstanding": p.Outstanding,
		"dead_letter": p.DeadLetter,
		"domains":     p.Domains,
	}
}

func freshnessTransitionsToSlice(transitions []FreshnessTransition, scoped bool) []map[string]any {
	result := make([]map[string]any, 0, len(transitions))
	for _, t := range transitions {
		row := map[string]any{
			"status":         t.Status,
			"trigger_kind":   t.TriggerKind,
			"freshness_hint": t.FreshnessHint,
			"observed_at":    nullableRFC3339(t.ObservedAt),
			"superseded_at":  nullableRFC3339(t.SupersededAt),
		}
		// Raw scope and generation identifiers correlate to internal rows and are
		// withheld from scoped callers; the lifecycle status stays visible.
		if !scoped {
			row["scope_id"] = t.ScopeID
			row["generation_id"] = t.GenerationID
		}
		result = append(result, row)
	}
	return result
}

func freshnessCausalityTruth(profile QueryProfile, fc FreshnessCausality, asOf time.Time) *TruthEnvelope {
	return &TruthEnvelope{
		Level:      TruthLevelExact,
		Capability: "freshness_causality.status",
		Profile:    profile,
		Basis:      TruthBasisRuntimeState,
		Freshness: TruthFreshness{
			State:      FreshnessFresh,
			ObservedAt: asOf.UTC().Format(time.RFC3339),
		},
		Reason: "resolved from runtime status snapshot freshness causality state " + fc.State,
	}
}
