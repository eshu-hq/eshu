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

	// readinessCache is the short-TTL cache for GET
	// /api/v0/status/collector-readiness. Zero value is a valid empty cache.
	// See collectorReadinessTTL and getCollectorReadiness for details.
	readinessCache collectorReadinessCache
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
