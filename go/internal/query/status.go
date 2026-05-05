package query

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// StatusHandler provides HTTP endpoints for pipeline status queries.
type StatusHandler struct {
	Neo4j        GraphQuery
	DB           *sql.DB
	StatusReader status.Reader
}

// Mount registers status query routes on the given mux.
func (h *StatusHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/status/pipeline", h.getPipelineStatus)
	mux.HandleFunc("GET /api/v0/status/collectors", h.listCollectors)
	mux.HandleFunc("GET /api/v0/status/ingesters", h.listIngesters)
	mux.HandleFunc("GET /api/v0/status/ingesters/{ingester}", h.getIngesterStatus)
	mux.HandleFunc("GET /api/v0/collectors", h.listCollectors)
	mux.HandleFunc("GET /api/v0/ingesters", h.listIngesters)
	mux.HandleFunc("GET /api/v0/ingesters/{ingester}", h.getIngesterStatus)
	mux.HandleFunc("GET /api/v0/status/index", h.getIndexStatus)
	mux.HandleFunc("GET /api/v0/index-status", h.getIndexStatus)
}

// getPipelineStatus returns the full pipeline status report from Postgres.
func (h *StatusHandler) getPipelineStatus(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	opts := status.DefaultOptions()
	report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), opts)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, statusReportToMap(report))
}

// listCollectors returns the known coordinator-managed collector instances.
func (h *StatusHandler) listCollectors(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	collectors := []map[string]any{}
	if report.Coordinator != nil {
		collectors = make([]map[string]any, 0, len(report.Coordinator.CollectorInstances))
		for _, instance := range report.Coordinator.CollectorInstances {
			collectors = append(collectors, map[string]any{
				"instance_id":      instance.InstanceID,
				"collector_kind":   instance.CollectorKind,
				"mode":             instance.Mode,
				"enabled":          instance.Enabled,
				"bootstrap":        instance.Bootstrap,
				"claims_enabled":   instance.ClaimsEnabled,
				"display_name":     instance.DisplayName,
				"last_observed_at": instance.LastObservedAt.Format(time.RFC3339),
				"updated_at":       instance.UpdatedAt.Format(time.RFC3339),
				"deactivated_at":   nullableRFC3339(instance.DeactivatedAt),
			})
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":    buildinfo.AppVersion(),
		"collectors": collectors,
		"count":      len(collectors),
	})
}

// listIngesters returns the known ingesters with basic health info.
func (h *StatusHandler) listIngesters(w http.ResponseWriter, r *http.Request) {
	ingesters := []map[string]any{
		{
			"name":           "repository",
			"runtime_family": "ingester",
			"aliases":        []string{"repository", "bootstrap-index", "repo-sync", "workspace-index"},
		},
	}

	// Enrich with live status if available
	if h.StatusReader != nil {
		report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
		if err == nil {
			ingesters[0]["health"] = report.Health.State
			ingesters[0]["queue_outstanding"] = report.Queue.Outstanding
			if report.Coordinator != nil {
				ingesters[0]["collector_instances"] = len(report.Coordinator.CollectorInstances)
			}
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":   buildinfo.AppVersion(),
		"ingesters": ingesters,
		"count":     len(ingesters),
	})
}

// getIngesterStatus returns detailed status for a specific ingester.
func (h *StatusHandler) getIngesterStatus(w http.ResponseWriter, r *http.Request) {
	ingester := PathParam(r, "ingester")
	if ingester == "" {
		WriteError(w, http.StatusBadRequest, "ingester name is required")
		return
	}

	// Validate known ingester
	knownIngesters := map[string]bool{"repository": true}
	if !knownIngesters[ingester] {
		WriteError(w, http.StatusNotFound, fmt.Sprintf("unknown ingester: %s", ingester))
		return
	}

	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":         buildinfo.AppVersion(),
		"ingester":        ingester,
		"runtime_family":  "ingester",
		"health":          healthToMap(report.Health),
		"queue":           queueToMap(report.Queue),
		"coordinator":     coordinatorToMap(report.Coordinator),
		"scope_activity":  scopeActivityToMap(report.ScopeActivity),
		"stage_summaries": stageSummariesToSlice(report.StageSummaries),
		"domain_backlogs": domainBacklogsToSlice(report.DomainBacklogs),
	})
}

// getIndexStatus returns the index status using the pipeline report as a proxy.
func (h *StatusHandler) getIndexStatus(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	// Query Neo4j for repository count if available
	var repoCount int
	if h.Neo4j != nil {
		row, qErr := h.Neo4j.RunSingle(r.Context(), "MATCH (r:Repository) RETURN count(r) as count", nil)
		if qErr == nil && row != nil {
			repoCount = IntVal(row, "count")
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":          buildinfo.AppVersion(),
		"status":           report.Health.State,
		"reasons":          report.Health.Reasons,
		"repository_count": repoCount,
		"queue":            queueToMap(report.Queue),
		"coordinator":      coordinatorToMap(report.Coordinator),
		"scope_activity":   scopeActivityToMap(report.ScopeActivity),
	})
}

// statusReportToMap converts a status.Report to a JSON-friendly map.
func statusReportToMap(r status.Report) map[string]any {
	result := map[string]any{
		"version":                buildinfo.AppVersion(),
		"as_of":                  r.AsOf.Format(time.RFC3339),
		"health":                 healthToMap(r.Health),
		"coordinator":            coordinatorToMap(r.Coordinator),
		"queue":                  queueToMap(r.Queue),
		"scope_activity":         scopeActivityToMap(r.ScopeActivity),
		"generation_history":     generationHistoryToMap(r.GenerationHistory),
		"generation_transitions": generationTransitionsToSlice(r.GenerationTransitions),
		"scope_totals":           r.ScopeTotals,
		"generation_totals":      r.GenerationTotals,
		"stage_summaries":        stageSummariesToSlice(r.StageSummaries),
		"domain_backlogs":        domainBacklogsToSlice(r.DomainBacklogs),
		"flow_summaries":         flowSummariesToSlice(r.FlowSummaries),
		"retry_policies":         retryPoliciesToSlice(r.RetryPolicies),
	}

	return result
}

// healthToMap converts a HealthSummary to a map.
func healthToMap(h status.HealthSummary) map[string]any {
	return map[string]any{
		"state":   h.State,
		"reasons": h.Reasons,
	}
}

// queueToMap converts a QueueSnapshot to a map.
func queueToMap(q status.QueueSnapshot) map[string]any {
	return map[string]any{
		"total":                     q.Total,
		"outstanding":               q.Outstanding,
		"pending":                   q.Pending,
		"in_flight":                 q.InFlight,
		"retrying":                  q.Retrying,
		"succeeded":                 q.Succeeded,
		"dead_letter":               q.DeadLetter,
		"failed":                    q.Failed,
		"oldest_outstanding_age":    q.OldestOutstandingAge.Seconds(),
		"oldest_outstanding_age_ms": q.OldestOutstandingAge.Milliseconds(),
		"overdue_claims":            q.OverdueClaims,
	}
}

// scopeActivityToMap converts a ScopeActivitySnapshot to a map.
func scopeActivityToMap(s status.ScopeActivitySnapshot) map[string]any {
	return map[string]any{
		"active":    s.Active,
		"changed":   s.Changed,
		"unchanged": s.Unchanged,
	}
}

// generationHistoryToMap converts a GenerationHistorySnapshot to a map.
func generationHistoryToMap(g status.GenerationHistorySnapshot) map[string]any {
	return map[string]any{
		"active":     g.Active,
		"pending":    g.Pending,
		"completed":  g.Completed,
		"superseded": g.Superseded,
		"failed":     g.Failed,
		"other":      g.Other,
	}
}

// stageSummariesToSlice converts []StageSummary to a slice of maps.
func stageSummariesToSlice(stages []status.StageSummary) []map[string]any {
	if len(stages) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(stages))
	for _, s := range stages {
		result = append(result, map[string]any{
			"stage":       s.Stage,
			"pending":     s.Pending,
			"claimed":     s.Claimed,
			"running":     s.Running,
			"retrying":    s.Retrying,
			"succeeded":   s.Succeeded,
			"dead_letter": s.DeadLetter,
			"failed":      s.Failed,
		})
	}
	return result
}

// domainBacklogsToSlice converts []DomainBacklog to a slice of maps.
func domainBacklogsToSlice(domains []status.DomainBacklog) []map[string]any {
	if len(domains) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(domains))
	for _, d := range domains {
		result = append(result, map[string]any{
			"domain":      d.Domain,
			"outstanding": d.Outstanding,
			"retrying":    d.Retrying,
			"dead_letter": d.DeadLetter,
			"failed":      d.Failed,
			"oldest_age":  d.OldestAge.Seconds(),
		})
	}
	return result
}

// flowSummariesToSlice converts []FlowSummary to a slice of maps.
func flowSummariesToSlice(flows []status.FlowSummary) []map[string]any {
	if len(flows) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(flows))
	for _, f := range flows {
		result = append(result, map[string]any{
			"lane":     f.Lane,
			"source":   f.Source,
			"progress": f.Progress,
			"backlog":  f.Backlog,
		})
	}
	return result
}

func coordinatorToMap(snapshot *status.CoordinatorSnapshot) map[string]any {
	if snapshot == nil {
		return map[string]any{}
	}

	instances := make([]map[string]any, 0, len(snapshot.CollectorInstances))
	for _, instance := range snapshot.CollectorInstances {
		instances = append(instances, map[string]any{
			"instance_id":      instance.InstanceID,
			"collector_kind":   instance.CollectorKind,
			"mode":             instance.Mode,
			"enabled":          instance.Enabled,
			"bootstrap":        instance.Bootstrap,
			"claims_enabled":   instance.ClaimsEnabled,
			"display_name":     instance.DisplayName,
			"last_observed_at": instance.LastObservedAt.Format(time.RFC3339),
			"updated_at":       instance.UpdatedAt.Format(time.RFC3339),
			"deactivated_at":   nullableRFC3339(instance.DeactivatedAt),
		})
	}

	return map[string]any{
		"collector_instances":     instances,
		"run_status_counts":       namedCountsToSlice(snapshot.RunStatusCounts),
		"work_item_status_counts": namedCountsToSlice(snapshot.WorkItemStatusCounts),
		"completeness_counts":     namedCountsToSlice(snapshot.CompletenessCounts),
		"active_claims":           snapshot.ActiveClaims,
		"overdue_claims":          snapshot.OverdueClaims,
		"oldest_pending_age":      snapshot.OldestPendingAge.Seconds(),
	}
}

func namedCountsToSlice(rows []status.NamedCount) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"name":  row.Name,
			"count": row.Count,
		})
	}
	return result
}

func nullableRFC3339(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.Format(time.RFC3339)
}

// retryPoliciesToSlice converts []RetryPolicySummary to a slice of maps.
func retryPoliciesToSlice(policies []status.RetryPolicySummary) []map[string]any {
	if len(policies) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(policies))
	for _, p := range policies {
		result = append(result, map[string]any{
			"stage":               p.Stage,
			"max_attempts":        p.MaxAttempts,
			"retry_delay":         p.RetryDelay.String(),
			"retry_delay_ms":      p.RetryDelay.Milliseconds(),
			"retry_delay_seconds": p.RetryDelay.Seconds(),
		})
	}
	return result
}

// generationTransitionsToSlice converts []GenerationTransitionSnapshot to a slice of maps.
func generationTransitionsToSlice(transitions []status.GenerationTransitionSnapshot) []map[string]any {
	if len(transitions) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(transitions))
	for _, t := range transitions {
		item := map[string]any{
			"scope_id":      t.ScopeID,
			"generation_id": t.GenerationID,
			"status":        t.Status,
			"trigger_kind":  t.TriggerKind,
		}

		if t.FreshnessHint != "" {
			item["freshness_hint"] = t.FreshnessHint
		}
		if !t.ObservedAt.IsZero() {
			item["observed_at"] = t.ObservedAt.Format(time.RFC3339)
		}
		if !t.ActivatedAt.IsZero() {
			item["activated_at"] = t.ActivatedAt.Format(time.RFC3339)
		}
		if !t.SupersededAt.IsZero() {
			item["superseded_at"] = t.SupersededAt.Format(time.RFC3339)
		}
		if t.CurrentActiveGenerationID != "" {
			item["current_active_generation_id"] = t.CurrentActiveGenerationID
		}

		result = append(result, item)
	}
	return result
}
