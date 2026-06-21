package query

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// StatusHandler provides HTTP endpoints for pipeline status queries.
type StatusHandler struct {
	Neo4j           GraphQuery
	DB              *sql.DB
	StatusReader    status.Reader
	GovernanceAudit GovernanceAuditSummaryReader
	Profile         QueryProfile
	Governance      GovernanceStatusConfig
	// NarrationPosture is an optional func that returns the current governed
	// answer-narration posture. When non-nil it overrides the DB-derived
	// AnswerNarration field from the status report, so that GET
	// /api/v0/status/answer-narration reflects the real governed posture
	// rather than a static default. When nil, the handler falls back to
	// status.DefaultAnswerNarrationStatus (Unavailable/disabled).
	NarrationPosture func() status.AnswerNarrationStatus
}

// Mount registers status query routes on the given mux.
func (h *StatusHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/status/pipeline", h.getPipelineStatus)
	mux.HandleFunc("GET /api/v0/status/operator-control-plane", h.getOperatorControlPlane)
	mux.HandleFunc("GET /api/v0/status/freshness-causality", h.getFreshnessCausality)
	mux.HandleFunc("GET /api/v0/status/collectors", h.listCollectors)
	mux.HandleFunc("GET /api/v0/status/ingesters", h.listIngesters)
	mux.HandleFunc("GET /api/v0/status/ingesters/{ingester}", h.getIngesterStatus)
	mux.HandleFunc("GET /api/v0/collectors", h.listCollectors)
	mux.HandleFunc("GET /api/v0/ingesters", h.listIngesters)
	mux.HandleFunc("GET /api/v0/ingesters/{ingester}", h.getIngesterStatus)
	mux.HandleFunc("GET /api/v0/status/index", h.getIndexStatus)
	mux.HandleFunc("GET /api/v0/index-status", h.getIndexStatus)
	mux.HandleFunc("GET /api/v0/status/hosted-readiness", h.getHostedReadiness)
	mux.HandleFunc("GET /api/v0/status/collector-readiness", h.getCollectorReadiness)
	mux.HandleFunc("GET /api/v0/collector-readiness", h.getCollectorReadiness)
	mux.HandleFunc("GET /api/v0/status/governance", h.getGovernanceStatus)
	mux.HandleFunc("GET /api/v0/status/semantic-extraction", h.getSemanticExtractionStatus)
	mux.HandleFunc("GET /api/v0/status/answer-narration", h.getAnswerNarrationStatus)
}

// getPipelineStatus returns the full pipeline status report from Postgres.
func (h *StatusHandler) getPipelineStatus(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	opts := status.DefaultOptions()
	raw, report, err := loadStatusReport(r.Context(), h.StatusReader, time.Now(), opts)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, statusReportToMapWithRaw(report, raw))
}

// listCollectors returns registered collectors plus direct runtime status evidence.
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

	runtimes := status.CollectorRuntimeStatuses(report)
	collectors := collectorRuntimeStatusesToSlice(runtimes)
	if scopedAuthContext(r.Context()) {
		collectors = scopedCollectorRuntimeStatusesToSlice(runtimes)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":              buildinfo.AppVersion(),
		"collectors":           collectors,
		"count":                len(collectors),
		"classification_basis": "workflow coordinator registration plus direct status and persisted fact evidence",
		"updated_at":           collectorRuntimeUpdatedAt(runtimes),
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

	// Enrich with live status if available. The ingester surface renders only
	// health, queue, and coordinator-instance counts, so it loads a filtered
	// snapshot that skips the fact_records aggregates (see issue #3368).
	if h.StatusReader != nil {
		_, report, err := loadStatusReportFiltered(
			r.Context(),
			h.StatusReader,
			time.Now(),
			status.DefaultOptions(),
			status.SnapshotSelection{IncludeCollectorFactEvidence: false, IncludeRegistryCollectors: false},
		)
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

	_, report, err := loadStatusReportFiltered(
		r.Context(),
		h.StatusReader,
		time.Now(),
		status.DefaultOptions(),
		status.SnapshotSelection{IncludeCollectorFactEvidence: false, IncludeRegistryCollectors: false},
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	coordinator := coordinatorToMap(report.Coordinator)
	if scopedAuthContext(r.Context()) {
		coordinator = scopedCoordinatorToMap(report.Coordinator)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":         buildinfo.AppVersion(),
		"ingester":        ingester,
		"runtime_family":  "ingester",
		"health":          healthToMap(report.Health),
		"queue":           queueToMap(report.Queue),
		"coordinator":     coordinator,
		"scope_activity":  scopeActivityToMap(report.ScopeActivity),
		"stage_summaries": stageSummariesToSlice(report.StageSummaries),
		"domain_backlogs": domainBacklogsToSlice(report.DomainBacklogs, report.QueueBlockages),
	})
}

// getIndexStatus returns the index status using the pipeline report as a proxy.
func (h *StatusHandler) getIndexStatus(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	// The index surface never renders registry collectors or collector fact
	// evidence, so it loads a filtered snapshot that skips the fact_records
	// aggregates those sections require. See GitHub issue #3368.
	selection := status.SnapshotSelection{
		IncludeCollectorFactEvidence: false,
		IncludeRegistryCollectors:    false,
	}
	raw, report, err := loadStatusReportFiltered(
		r.Context(),
		h.StatusReader,
		time.Now(),
		status.DefaultOptions(),
		selection,
	)
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

	payload := map[string]any{
		"version":             buildinfo.AppVersion(),
		"status":              report.Health.State,
		"reasons":             report.Health.Reasons,
		"repository_count":    repoCount,
		"queue":               queueToMap(report.Queue),
		"queue_blockages":     queueBlockagesToSlice(raw.QueueBlockages),
		"coordinator":         coordinatorToMap(report.Coordinator),
		"scope_activity":      scopeActivityToMap(report.ScopeActivity),
		"aws_materialization": awsMaterializationStatusToMap(raw.DomainBacklogs, raw.QueueBlockages),
		"semantic_extraction": semanticExtractionStatusToMap(report.SemanticExtraction),
	}
	payload["terraform_state"] = terraformStateStatusToMap(report.TerraformState)
	WriteJSON(w, http.StatusOK, payload)
}

func loadStatusReport(
	ctx context.Context,
	reader status.Reader,
	asOf time.Time,
	opts status.Options,
) (status.RawSnapshot, status.Report, error) {
	return loadStatusReportFiltered(ctx, reader, asOf, opts, status.FullSnapshotSelection())
}

func loadStatusReportFiltered(
	ctx context.Context,
	reader status.Reader,
	asOf time.Time,
	opts status.Options,
	selection status.SnapshotSelection,
) (status.RawSnapshot, status.Report, error) {
	if reader == nil {
		return status.RawSnapshot{}, status.Report{}, fmt.Errorf("status reader is required")
	}
	raw, err := reader.ReadStatusSnapshotFiltered(ctx, asOf.UTC(), selection)
	if err != nil {
		return status.RawSnapshot{}, status.Report{}, fmt.Errorf("read status snapshot: %w", err)
	}
	return raw, status.BuildReport(raw, opts), nil
}

// statusReportToMap converts a status.Report to a JSON-friendly map.
func statusReportToMap(r status.Report) map[string]any {
	return statusReportToMapWithAWS(r, r.DomainBacklogs, r.QueueBlockages)
}

func statusReportToMapWithRaw(r status.Report, raw status.RawSnapshot) map[string]any {
	return statusReportToMapWithAWS(r, raw.DomainBacklogs, raw.QueueBlockages)
}

func statusReportToMapWithAWS(
	r status.Report,
	awsDomains []status.DomainBacklog,
	awsBlockages []status.QueueBlockage,
) map[string]any {
	result := map[string]any{
		"version":                           buildinfo.AppVersion(),
		"as_of":                             r.AsOf.Format(time.RFC3339),
		"health":                            healthToMap(r.Health),
		"coordinator":                       coordinatorToMap(r.Coordinator),
		"collector_runtimes":                collectorRuntimeStatusesToSlice(status.CollectorRuntimeStatuses(r)),
		"queue":                             queueToMap(r.Queue),
		"scope_activity":                    scopeActivityToMap(r.ScopeActivity),
		"generation_history":                generationHistoryToMap(r.GenerationHistory),
		"generation_transitions":            generationTransitionsToSlice(r.GenerationTransitions),
		"scope_totals":                      r.ScopeTotals,
		"generation_totals":                 r.GenerationTotals,
		"stage_summaries":                   stageSummariesToSlice(r.StageSummaries),
		"domain_backlogs":                   domainBacklogsToSlice(r.DomainBacklogs, r.QueueBlockages),
		"queue_blockages":                   queueBlockagesToSlice(r.QueueBlockages),
		"aws_materialization":               awsMaterializationStatusToMap(awsDomains, awsBlockages),
		"semantic_extraction":               semanticExtractionStatusToMap(r.SemanticExtraction),
		"collector_generation_dead_letters": collectorGenerationDeadLettersToMap(r.CollectorGenerationDeadLetters),
		"flow_summaries":                    flowSummariesToSlice(r.FlowSummaries),
		"retry_policies":                    retryPoliciesToSlice(r.RetryPolicies),
	}
	result["terraform_state"] = terraformStateStatusToMap(r.TerraformState)

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

func collectorGenerationDeadLettersToMap(
	s status.CollectorGenerationDeadLetterSnapshot,
) map[string]any {
	return map[string]any{
		"dead_letter":               s.DeadLetter,
		"replay_requested":          s.ReplayRequested,
		"replay_attempts":           s.ReplayAttempts,
		"oldest_dead_letter_age":    s.OldestDeadLetterAge.Seconds(),
		"oldest_dead_letter_age_ms": s.OldestDeadLetterAge.Milliseconds(),
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
func domainBacklogsToSlice(domains []status.DomainBacklog, blockages []status.QueueBlockage) []map[string]any {
	if len(domains) == 0 {
		return []map[string]any{}
	}

	blockedByDomain := queueBlockageCountsByDomain(blockages)
	result := make([]map[string]any, 0, len(domains))
	for _, d := range domains {
		result = append(result, domainBacklogToMap(d, domainBacklogBuckets(d, blockedByDomain[d.Domain])))
	}
	return result
}

func queueBlockagesToSlice(blockages []status.QueueBlockage) []map[string]any {
	if len(blockages) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(blockages))
	for _, blockage := range blockages {
		result = append(result, map[string]any{
			"stage":           blockage.Stage,
			"domain":          blockage.Domain,
			"conflict_domain": blockage.ConflictDomain,
			"conflict_key":    blockage.ConflictKey,
			"blocked":         blockage.Blocked,
			"oldest_age":      blockage.OldestAge.Seconds(),
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

	backpressure := make([]map[string]any, 0, len(snapshot.CollectorBackpressure))
	for _, bp := range snapshot.CollectorBackpressure {
		backpressure = append(backpressure, map[string]any{
			"collector_kind":        bp.CollectorKind,
			"collector_instance_id": bp.CollectorInstanceID,
			"pending":               bp.Pending,
			"claimed":               bp.Claimed,
			"retrying":              bp.Retrying,
			"dead_letter":           bp.DeadLetter,
		})
	}

	result := map[string]any{
		"collector_instances":     instances,
		"collector_backpressure":  backpressure,
		"run_status_counts":       namedCountsToSlice(snapshot.RunStatusCounts),
		"work_item_status_counts": namedCountsToSlice(snapshot.WorkItemStatusCounts),
		"completeness_counts":     namedCountsToSlice(snapshot.CompletenessCounts),
		"active_claims":           snapshot.ActiveClaims,
		"overdue_claims":          snapshot.OverdueClaims,
		"oldest_pending_age":      snapshot.OldestPendingAge.Seconds(),
	}
	if recent := snapshot.RecentFailures; recent != nil {
		result["recent_failures"] = map[string]any{
			"window_seconds":       recent.Window.Seconds(),
			"failed_runs":          recent.FailedRuns,
			"blocked_completeness": recent.BlockedCompleteness,
			"terminal_work_items":  recent.TerminalWorkItems,
		}
	}
	return result
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
