// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// LiveActivityReader reads the bounded in-flight work-item read model backing
// GET /api/v0/status/operations (issue #5137). It is a narrow port, separate
// from status.Reader, because the query is bounded by an operator-supplied
// limit rather than the fixed RawSnapshot shape the rest of the status
// surface composes.
type LiveActivityReader interface {
	ReadLiveActivity(ctx context.Context, limit int) ([]status.LiveActivityRow, bool, error)
}

// operationsDefaultLimit and operationsMaxLimit bound the `limit` query
// parameter on GET /api/v0/status/operations. They mirror
// pgstatus.LiveActivityDefaultLimit/LiveActivityMaxLimit so the HTTP contract
// and the storage bound stay in lockstep without duplicating the numbers.
const (
	operationsDefaultLimit = pgstatus.LiveActivityDefaultLimit
	operationsMaxLimit     = pgstatus.LiveActivityMaxLimit
)

// scopedOperationsRoute reports whether the request targets the live
// operations board. The handler redacts repo identity (source_key,
// source_display) and worker identity (lease_owner) from live_activity rows
// for scoped tokens and collapses collector detail to aggregate counts, so
// the route is tenant-filter safe.
func scopedOperationsRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/operations"
}

// getOperations returns the live operations board read model: health,
// collector runtimes (with heartbeat), stage summaries, domain backlogs, and
// queue pressure -- all projected from the same status snapshot the rest of
// the status surface uses -- composed with live_activity, a bounded,
// separately-queried list of in-flight work items joined to their
// originating repo and worker.
//
// Scoped tokens receive the same aggregate sections; live_activity rows keep
// every field except source_key/source_display (repo identity, raw and
// human-readable) and lease_owner (worker identity), which are withheld, and
// collectors collapse to the existing aggregate-only projection used by the
// collector-status route.
func (h *StatusHandler) getOperations(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}
	if h.LiveActivity == nil {
		WriteError(w, http.StatusServiceUnavailable, "live activity reader not configured")
		return
	}

	limit, ok := operationsLimit(w, r)
	if !ok {
		return
	}

	report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	activity, truncated, err := h.LiveActivity.ReadLiveActivity(r.Context(), limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("read live activity: %v", err))
		return
	}

	ops := status.Operations(report, activity, truncated, limit)
	WriteJSON(w, http.StatusOK, operationsToMap(ops, scopedAuthContext(r.Context())))
}

// operationsLimit resolves and validates the `limit` query parameter,
// defaulting to operationsDefaultLimit when absent and rejecting values
// outside 1..operationsMaxLimit with a 400.
func operationsLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return operationsDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > operationsMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", operationsMaxLimit))
		return 0, false
	}
	return limit, true
}

// operationsToMap renders the operations-board read model to a JSON-friendly
// map. When scoped is true, live_activity rows withhold source_key,
// source_display, and lease_owner, and collectors collapse to the
// aggregate-only projection.
func operationsToMap(ops status.OperationsReport, scoped bool) map[string]any {
	collectors := collectorRuntimeStatusesToSlice(ops.Collectors)
	if scoped {
		collectors = scopedCollectorRuntimeStatusesToSlice(ops.Collectors)
	}

	return map[string]any{
		"version":         buildinfo.AppVersion(),
		"as_of":           ops.AsOf.Format(time.RFC3339),
		"scoped":          scoped,
		"health":          healthToMap(ops.Health),
		"collectors":      collectors,
		"stage_summaries": stageSummariesToSlice(ops.StageSummaries),
		"domain_backlogs": domainBacklogsToSlice(ops.DomainBacklogs, nil),
		"queue":           queueToMap(ops.Queue),
		"live_activity":   liveActivityRowsToSlice(ops.LiveActivity, ops.AsOf, scoped),
		"truncated":       ops.Truncated,
		"limit":           ops.Limit,
	}
}

// liveActivityRowsToSlice converts []status.LiveActivityRow to the wire
// shape, adding an as-of-relative age. Scoped callers never see source_key
// or source_display (repo identity, raw and human-readable) or lease_owner
// (worker identity); every other field (stage, status, domain, attempt
// count, age, scope/collector kind) stays visible since it carries no
// cross-tenant identity.
func liveActivityRowsToSlice(rows []status.LiveActivityRow, asOf time.Time, scoped bool) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		leaseOwner := row.LeaseOwner
		sourceKey := row.SourceKey
		sourceDisplay := row.SourceDisplay
		if scoped {
			leaseOwner = ""
			sourceKey = ""
			sourceDisplay = ""
		}
		result = append(result, map[string]any{
			"work_item_id":   row.WorkItemID,
			"stage":          row.Stage,
			"status":         row.Status,
			"domain":         row.Domain,
			"lease_owner":    leaseOwner,
			"claim_until":    nullableRFC3339(row.ClaimUntil),
			"attempt_count":  row.AttemptCount,
			"updated_at":     nullableRFC3339(row.UpdatedAt),
			"created_at":     nullableRFC3339(row.CreatedAt),
			"age_seconds":    row.Age(asOf).Seconds(),
			"scope_kind":     row.ScopeKind,
			"collector_kind": row.CollectorKind,
			"source_system":  row.SourceSystem,
			"source_key":     sourceKey,
			"source_display": sourceDisplay,
		})
	}
	return result
}
