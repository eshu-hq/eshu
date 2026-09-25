// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func scopedAuthContext(ctx context.Context) bool {
	return auth.ScopedAuthContext(ctx)
}

func scopedCoordinatorToMap(snapshot *status.CoordinatorSnapshot) map[string]any {
	if snapshot == nil {
		return map[string]any{}
	}

	result := map[string]any{
		"collector_instance_count": len(snapshot.CollectorInstances),
		"run_status_counts":        namedCountsToSlice(snapshot.RunStatusCounts),
		"work_item_status_counts":  namedCountsToSlice(snapshot.WorkItemStatusCounts),
		"completeness_counts":      namedCountsToSlice(snapshot.CompletenessCounts),
		"active_claims":            snapshot.ActiveClaims,
		"overdue_claims":           snapshot.OverdueClaims,
		"oldest_pending_age":       snapshot.OldestPendingAge.Seconds(),
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

// indexStatusScopedCompleteness names the scoped index-status payload shape in
// completeness_state, the same disclosure getOperations uses for
// scoped_live_activity_only (#5137).
const indexStatusScopedCompleteness = "scoped_repository_count_only"

// indexStatusWithheldSections lists every key the unscoped index-status
// payload carries beyond version and repository_count. A scoped caller is told
// these were withheld rather than left to infer a partial report is the whole
// one. TestGetIndexStatusScopedBodyWithholdsDeploymentWideSections derives the
// expected list from a real unscoped response, so adding a section to
// getIndexStatus without listing it here fails the build.
var indexStatusWithheldSections = []string{
	"status",
	"reasons",
	"queue",
	"queue_blockages",
	"coordinator",
	"scope_activity",
	"aws_materialization",
	"semantic_extraction",
	"terraform_state",
}

// indexStatusScopedRepositoryCountCypher counts only the Repository nodes the
// caller's grant names, inside the statement so no ungranted count is ever
// produced and filtered afterwards. The predicate is
// RepositoryAccessFilter.GraphCondition, the binding the repository list count
// (repository.queryRepositoryTotal) already ships.
func indexStatusScopedRepositoryCountCypher(access querycontract.RepositoryAccessFilter) string {
	return "MATCH (r:Repository) " + access.GraphWhereClause("r") + " RETURN count(r) AS count"
}

// serveIndexStatus is the handler behind GET /api/v0/index-status and GET
// /api/v0/status/index. A scoped caller (#5167) is answered by
// getScopedIndexStatus and never reaches the process-global status snapshot;
// every other caller gets the unchanged deployment-wide report from
// getIndexStatus.
func (h *StatusHandler) serveIndexStatus(w http.ResponseWriter, r *http.Request) {
	if access := querycontract.RepositoryAccessFilterFromContext(r.Context()); access.Scoped() {
		h.getScopedIndexStatus(w, r, access)
		return
	}
	h.getIndexStatus(w, r)
}

// getScopedIndexStatus serves GET /api/v0/index-status and GET
// /api/v0/status/index to a scoped caller (#5167). The status snapshot behind
// the unscoped report is process-global: its queue, coordinator, scope-activity,
// AWS-materialization, semantic and Terraform-state sections cannot be
// attributed to the caller's grants, and a queue_blockages row reports
// conflict_key as COALESCE(conflict_key, scope_id), a raw scope id. They are
// withheld whole, the way getOperations (#5137) withholds its process-global
// aggregates, and are never read.
//
// repository_count is bound to the grant in Cypher. A scoped caller with no
// granted repository or scope answers 0 without a graph call. A missing graph
// or a failed count is an error rather than 0, because zero is a valid and
// materially different answer for a scoped caller.
func (h *StatusHandler) getScopedIndexStatus(
	w http.ResponseWriter,
	r *http.Request,
	access querycontract.RepositoryAccessFilter,
) {
	repoCount := 0
	if !access.Empty() {
		if h.Neo4j == nil {
			WriteError(w, http.StatusServiceUnavailable, "graph backend not configured")
			return
		}
		row, err := h.Neo4j.RunSingle(
			r.Context(),
			indexStatusScopedRepositoryCountCypher(access),
			access.GraphParams(nil),
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("count granted repositories: %v", err))
			return
		}
		if row == nil {
			WriteError(w, http.StatusInternalServerError, "count granted repositories: count query returned no row")
			return
		}
		repoCount = IntVal(row, "count")
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"version":            buildinfo.AppVersion(),
		"scoped":             true,
		"repository_count":   repoCount,
		"completeness_state": indexStatusScopedCompleteness,
		"withheld_sections":  indexStatusWithheldSections,
	})
}
