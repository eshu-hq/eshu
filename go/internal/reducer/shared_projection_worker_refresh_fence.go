package reducer

import (
	"context"
	"fmt"
	"strings"
)

// repoRefreshIntentType marks a shared-projection intent whose only job is to
// issue the single repo-wide retract for a repo-wide-retract domain
// (handles_route, runs_in, invokes_cloud_action). It carries no edge of its own;
// filterUpsertRows drops it from writes because its action is repoRefreshAction.
const (
	repoRefreshIntentType = "repo_refresh"
	repoRefreshAction     = "refresh"
	// retractViaRefreshKey marks a per-edge row that was emitted WITH a paired
	// repo refresh intent, so the worker may safely fence it behind that refresh.
	// Per-edge rows without the marker predate the #2898 emission (no paired
	// refresh exists for their source run), so the worker keeps them on the legacy
	// per-partition retract path rather than deferring them forever. Such in-flight
	// rows drain normally and are superseded by the next re-ingest's marked rows.
	retractViaRefreshKey = "retract_via_refresh"
)

// domainHasRepoWideRetract reports whether a domain owns its retract at the
// repository (or whole-repo delta) level rather than per partition. These domains
// emit per-edge partition keys, so their edges spread across partitions; the
// generic worker would otherwise issue the same scope-wide retract once per
// partition and wipe sibling partitions' just-written edges within a cycle
// (#2910). The retract suppression (#2898) routes the single retract through a
// per-repo refresh intent and fences per-edge writes behind it.
//
// The retract the refresh owns may be repo-wide (delete every edge for the repo)
// or file-scoped (delete only the changed files' edges on a delta generation):
// inheritance_edges, sql_relationships, and rationale_edges retract repo-wide by
// default and file-scoped under a delta, while the three symbol→runtime domains
// always retract repo-wide. The fence mechanism is identical either way — the refresh
// intent owns the single retract and the per-edge writes are deferred until it
// commits — because the refresh carries whichever delta scope the materializer
// attached. Repo-keyed domains (platform_infra, workload_dependency, …) keep one
// partition per repo, so they do not spread and are intentionally excluded.
func domainHasRepoWideRetract(domain string) bool {
	switch domain {
	case DomainHandlesRoute, DomainRunsIn, DomainInvokesCloudAction, DomainInheritanceEdges, DomainSQLRelationships, DomainShellExec, DomainRationaleEdges:
		return true
	default:
		return false
	}
}

// repoWideRetractRefreshPartitionKey is the whole-scope partition key the per-repo
// refresh intent is emitted under and that the worker reconstructs to fence a
// per-edge row. A whole-scope key hashes to exactly one partition, so a repo's
// single repo-wide retract is owned by one partition lease and cannot race
// itself. Emission (buildRepoWideRetractRefreshIntents) and the fence
// (perEdgeRowFenced) MUST build the key identically, so they share this helper.
func repoWideRetractRefreshPartitionKey(domain, repoID string) string {
	return domain + ":refresh:v1:whole:" + strings.TrimSpace(repoID)
}

// isRepoRefreshRow reports whether a row is a per-repo refresh intent.
func isRepoRefreshRow(row SharedProjectionIntentRow) bool {
	return payloadStr(row.Payload, "intent_type") == repoRefreshIntentType
}

// markRowsRetractViaRefresh stamps the retract_via_refresh marker on every
// per-edge row so the worker fences them behind their paired repo refresh intent.
// It is applied at emission, right where the refresh intents are built, so the
// marker and the refresh intent are always emitted together.
func markRowsRetractViaRefresh(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	for i := range rows {
		if rows[i].Payload == nil {
			rows[i].Payload = map[string]any{}
		}
		rows[i].Payload[retractViaRefreshKey] = true
	}
	return rows
}

// rowUsesRefreshFence reports whether a per-edge row opted into the repo-wide
// retract fence by carrying the retract_via_refresh marker its paired refresh
// intent guarantees. Rows without it predate #2898 emission and stay on the
// legacy per-partition retract path.
func rowUsesRefreshFence(row SharedProjectionIntentRow) bool {
	return payloadBool(row.Payload, retractViaRefreshKey)
}

// splitRepoRefreshRows separates per-repo refresh rows from per-edge rows,
// preserving order. A refresh row carries no edge target, so callers exempt it
// from the endpoint-presence (terminal) gate that would otherwise drain it with
// no edge and never run its repo-wide retract.
func splitRepoRefreshRows(rows []SharedProjectionIntentRow) (refresh, edge []SharedProjectionIntentRow) {
	for _, row := range rows {
		if isRepoRefreshRow(row) {
			refresh = append(refresh, row)
			continue
		}
		edge = append(edge, row)
	}
	return refresh, edge
}

// SharedProjectionRefreshFenceLookup reports whether a repo's whole-scope refresh
// partition has already completed for the current source run and domain. It is
// the durable happens-before signal that lets a per-edge upsert row write only
// after the single repo-wide retract has committed, even when partitions are
// processed concurrently across workers or replicas (#2898). The runtime
// SharedIntentStore satisfies it via
// HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents.
type SharedProjectionRefreshFenceLookup interface {
	HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(
		ctx context.Context,
		key SharedProjectionAcceptanceKey,
		partitionKey string,
		domain string,
	) (bool, error)
}

// repoWideRetractPlan is the split of a repo-wide-retract domain's selected batch
// into the rows that retract, the rows that write, the rows to mark completed,
// and the count of per-edge rows held by the refresh fence this cycle.
type repoWideRetractPlan struct {
	retractRows   []SharedProjectionIntentRow
	writeRows     []SharedProjectionIntentRow
	completedRows []SharedProjectionIntentRow
	deferred      int
}

// planRepoWideRetractWork splits a repo-wide-retract domain's ready rows so the
// repo-wide retract is issued only by the per-repo refresh intent, and per-edge
// rows write only once that refresh has retracted (#2898/#2910). It is called
// only when a fence lookup is wired and domainHasRepoWideRetract(domain) is true.
//
// Within one partition cycle a refresh row retracts (repo-wide) before any write
// happens, so per-edge rows for a repo whose refresh is in this same batch are
// safe to write now. Per-edge rows whose refresh lives in another partition are
// written only after the durable fence reports that refresh completed; otherwise
// they are deferred (left pending, not written, not completed) and re-selected
// next cycle. A refresh row never writes (filterUpsertRows drops it).
func planRepoWideRetractWork(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	fence SharedProjectionRefreshFenceLookup,
) (repoWideRetractPlan, error) {
	plan := repoWideRetractPlan{}
	refreshReposInBatch := make(map[string]struct{})
	for _, row := range rows {
		if isRepoRefreshRow(row) {
			refreshReposInBatch[sharedProjectionRowRepoID(row)] = struct{}{}
		}
	}

	for _, row := range rows {
		if isRepoRefreshRow(row) {
			plan.retractRows = append(plan.retractRows, row)
			plan.completedRows = append(plan.completedRows, row)
			continue
		}

		if !rowUsesRefreshFence(row) {
			// Legacy in-flight row (no paired refresh): keep the pre-#2898
			// per-partition retract so it drains instead of deferring forever. It is
			// superseded by the next re-ingest's fenced, marked rows.
			plan.retractRows = append(plan.retractRows, row)
			plan.writeRows = append(plan.writeRows, row)
			plan.completedRows = append(plan.completedRows, row)
			continue
		}

		repoID := sharedProjectionRowRepoID(row)
		if _, refreshHere := refreshReposInBatch[repoID]; refreshHere {
			// The refresh for this repo retracts earlier in this same cycle, so the
			// write is already ordered after it.
			plan.writeRows = append(plan.writeRows, row)
			plan.completedRows = append(plan.completedRows, row)
			continue
		}

		ready, err := perEdgeRowReady(ctx, domain, row, fence)
		if err != nil {
			return repoWideRetractPlan{}, err
		}
		if !ready {
			plan.deferred++
			continue
		}
		plan.writeRows = append(plan.writeRows, row)
		plan.completedRows = append(plan.completedRows, row)
	}

	return plan, nil
}

// perEdgeRowReady reports whether a per-edge row may write now: true once its
// repo's whole-scope refresh partition has completed for this source run. A row
// without a resolvable acceptance key is treated as ready so it cannot wedge the
// backlog; such a row is dropped earlier by authoritative-generation filtering in
// normal operation.
func perEdgeRowReady(
	ctx context.Context,
	domain string,
	row SharedProjectionIntentRow,
	fence SharedProjectionRefreshFenceLookup,
) (bool, error) {
	key, ok := row.AcceptanceKey()
	if !ok {
		return true, nil
	}
	refreshKey := repoWideRetractRefreshPartitionKey(domain, sharedProjectionRowRepoID(row))
	done, err := fence.HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(ctx, key, refreshKey, domain)
	if err != nil {
		return false, fmt.Errorf("check repo refresh fence for %s: %w", domain, err)
	}
	return done, nil
}
