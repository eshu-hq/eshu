// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// RepoRefreshIntentType marks a shared-projection intent whose only job is to
// issue the single repo-wide retract for a repo-wide-retract domain. Five
// emitters reference this constant: inheritance/intents.go,
// rationale_edge_intents.go, shell_exec_intents.go,
// sqlrelationship/sql_relationship_intents.go and
// symbol_runtime_refresh_intents.go. The rationale one is what the
// graph-write side below keys on. It carries no edge of its own;
// filterUpsertRows drops it from writes because its action is
// repoRefreshAction.
//
// Four further production sites spell the same value as a hard-coded literal
// rather than through this constant. Two EMIT it --
// code/call/intents.go and code_call_projection_work.go -- and
// two COMPARE against it: code_call_projection_partitions.go, and
// storage/postgres/shared_intents_history.go, where it decides
// rowCanBeCoveredByFileRefresh. All four are DomainCodeCalls-side and never
// reach collectWholeScopeRefreshRepoIDs, so none is a live drift hazard for the
// rationale guard, but they are copies and should migrate to this constant. A
// drifted comparison site is the same hazard as a drifted emitter: it fails
// silently, matching nothing rather than erroring.
//
// Exported because the graph-write side reads it back: storage/cypher's
// rationale retract collects whole-scope repository ids by matching this
// intent_type, and if the two sides drifted that predicate would match nothing,
// the whole-scope retract would silently stop running, and stale EXPLAINS edges
// would persist with no error and no dead letter. One definition shared by that
// predicate and the five emitters above is what keeps that from being possible
// (#5998); the four literal sites noted above are the remaining exception.
const (
	// RepoRefreshIntentType aliases [sharedintent.RepoRefreshIntentType].
	RepoRefreshIntentType = sharedintent.RepoRefreshIntentType
	// repoRefreshAction aliases [sharedintent.RepoRefreshAction].
	repoRefreshAction = sharedintent.RepoRefreshAction
	// retractViaRefreshKey aliases [sharedintent.RetractViaRefreshKey].
	retractViaRefreshKey = sharedintent.RetractViaRefreshKey
)

// domainHasRepoWideRetract forwards to [sharedintent.DomainHasRepoWideRetract].
// See that function for which domains are fenced and why: these domains emit
// per-edge partition keys, so their edges spread across partitions, and the
// retract suppression (#2898/#2910) routes their single repo-wide retract
// through a per-repo refresh intent instead of reissuing it once per
// partition. A second copy of the fenced set lives in
// internal/storage/cypher's wholeScopeRetractDomains table; a domain added
// here but missed there gets the #6166 over-delete.
func domainHasRepoWideRetract(domain string) bool {
	return sharedintent.DomainHasRepoWideRetract(domain)
}

// RepoWideRetractDomains forwards to [sharedintent.RepoWideRetractDomains].
func RepoWideRetractDomains() []string {
	return sharedintent.RepoWideRetractDomains()
}

// repoWideRetractRefreshPartitionKey is the whole-scope partition key the per-repo
// refresh intent is emitted under and that the worker reconstructs to fence a
// per-edge row. A whole-scope key hashes to exactly one partition, so a repo's
// single repo-wide retract is owned by one partition lease and cannot race
// itself. Emission (buildRepoWideRetractRefreshIntents) and the fence
// (perEdgeRowFenced) MUST build the key identically, so they share this helper.
func repoWideRetractRefreshPartitionKey(domain, repoID string) string {
	return sharedintent.RepoWideRetractRefreshPartitionKey(domain, repoID)
}

// isRepoRefreshRow forwards to [sharedintent.IsRepoRefreshRow].
func isRepoRefreshRow(row SharedProjectionIntentRow) bool {
	return sharedintent.IsRepoRefreshRow(row)
}

// markRowsRetractViaRefresh forwards to [sharedintent.MarkRowsRetractViaRefresh].
func markRowsRetractViaRefresh(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	return sharedintent.MarkRowsRetractViaRefresh(rows)
}

// rowUsesRefreshFence forwards to [worker.RowUsesRefreshFence].
func rowUsesRefreshFence(row SharedProjectionIntentRow) bool {
	return worker.RowUsesRefreshFence(row)
}

// SharedProjectionRefreshFenceLookup is the root spelling of
// [worker.RefreshFenceLookup].
type SharedProjectionRefreshFenceLookup = worker.RefreshFenceLookup

// FirstProjectionLookup is the root spelling of [worker.FirstProjectionLookup].
type FirstProjectionLookup = worker.FirstProjectionLookup

// repoWideRetractPlan is the root spelling of [worker.RepoWideRetractPlan].
type repoWideRetractPlan = worker.RepoWideRetractPlan

// planRepoWideRetractWork forwards to [worker.PlanRepoWideRetractWork].
func planRepoWideRetractWork(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	fence SharedProjectionRefreshFenceLookup,
	firstProjection FirstProjectionLookup,
	logger *slog.Logger,
) (repoWideRetractPlan, error) {
	return worker.PlanRepoWideRetractWork(ctx, domain, rows, fence, firstProjection, logger)
}
