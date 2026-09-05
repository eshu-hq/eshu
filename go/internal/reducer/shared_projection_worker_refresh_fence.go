// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	log "github.com/eshu-hq/eshu/go/pkg/log"
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
// code_call_materialization_intents.go and code_call_projection_work.go -- and
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
// The set is written down once, here, because a second copy of it lives in
// another package: storage/cypher's wholeScopeRetractDomains table splits these
// same domains into the narrowed and un-narrowed halves of the whole-scope
// retract. A domain added to the fence but missed there gets a
// whole-repository DELETE bound to the batch-wide repository list, which is the
// #6166 over-delete, and nothing in that package's tests would iterate over it.
// The predicate and RepoWideRetractDomains read this one map so
// TestWholeScopeRetractDomainsCoversFencedSet compares the two sets rather than
// two hand-typed lists that happen to agree today.
var repoWideRetractDomains = map[string]struct{}{
	DomainHandlesRoute:       {},
	DomainRunsIn:             {},
	DomainInvokesCloudAction: {},
	DomainInheritanceEdges:   {},
	DomainSQLRelationships:   {},
	DomainShellExec:          {},
	DomainRationaleEdges:     {},
}

func domainHasRepoWideRetract(domain string) bool {
	_, fenced := repoWideRetractDomains[domain]
	return fenced
}

// RepoWideRetractDomains returns every domain whose retract the per-repo refresh
// intent owns, sorted, so another package can check its own handling of that set
// against this one instead of re-enumerating it. It reads the same map
// domainHasRepoWideRetract does, so the two cannot disagree.
func RepoWideRetractDomains() []string {
	domains := make([]string, 0, len(repoWideRetractDomains))
	for domain := range repoWideRetractDomains {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
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

// isRepoRefreshRow reports whether a row is a per-repo refresh intent.
func isRepoRefreshRow(row SharedProjectionIntentRow) bool {
	return payloadStr(row.Payload, "intent_type") == RepoRefreshIntentType
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
	return payloadcore.PayloadBool(row.Payload, retractViaRefreshKey)
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

// SharedProjectionRefreshFenceLookup reports whether a repo's whole-scope
// refresh partition has completed for the current generation. It is the durable
// happens-before signal that lets a per-edge upsert row write only after the
// single repo-wide retract for its generation has committed, even when
// partitions are processed concurrently across workers or replicas (#2898,
// #5554). Exact same-generation redelivery is idempotent because intent IDs are
// deterministic and completed rows are not reopened by the durable upsert.
type SharedProjectionRefreshFenceLookup interface {
	HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
		ctx context.Context,
		key SharedProjectionAcceptanceKey,
		generationID string,
		partitionKey string,
		domain string,
	) (bool, error)
}

// FirstProjectionLookup reports whether a scope has any generation other than
// the current one (in any status). When it reports false the scope's only
// generation is the current one — a true first projection — so its whole-scope
// edge retract is a guaranteed no-op and is skipped (#3624). It deliberately
// does not key on activation: these domains write edges on acceptance, before a
// generation activates, so a superseded-while-pending generation can have
// written edges without ever setting activated_at; "no other generation exists"
// is the correct zero-prior-edges signal. A nil lookup disables the skip,
// leaving the retract byte-identical to prior behavior.
type FirstProjectionLookup interface {
	ScopeHasPriorGeneration(ctx context.Context, scopeID, currentGenerationID string) (bool, error)
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
//
// firstProjection additionally lets a refresh row skip its whole-scope retract
// entirely (#3624): when the row's scope has no generation other than the
// current one, this is the scope's first projection, so there are zero prior edges and the retract
// is a guaranteed no-op. The row still lands in plan.completedRows so the fence
// opens and per-edge writes proceed; only the (expensive, full-scan-on-NornicDB)
// retract call is skipped. A nil firstProjection disables the skip, leaving the
// retract byte-identical to prior behavior. The probe is memoized per scope ID
// within one call so a batch with many refresh rows for the same scope costs at
// most one lookup. logger is optional; when set, a skip is logged as an operator
// signal.
func planRepoWideRetractWork(
	ctx context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	fence SharedProjectionRefreshFenceLookup,
	firstProjection FirstProjectionLookup,
	logger *slog.Logger,
) (repoWideRetractPlan, error) {
	plan := repoWideRetractPlan{}
	refreshReposInBatch := make(map[string]struct{})
	for _, row := range rows {
		if isRepoRefreshRow(row) {
			refreshReposInBatch[sharedProjectionRowRepoID(row)] = struct{}{}
		}
	}

	firstProjectionMemo := make(map[string]bool)

	for _, row := range rows {
		if isRepoRefreshRow(row) {
			plan.completedRows = append(plan.completedRows, row)
			skip, err := skipFirstProjectionRetract(ctx, domain, row, firstProjection, firstProjectionMemo, logger)
			if err != nil {
				return repoWideRetractPlan{}, err
			}
			if !skip {
				plan.retractRows = append(plan.retractRows, row)
			}
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

// skipFirstProjectionRetract reports whether a refresh row's whole-scope
// retract may be skipped because the scope has no generation other than the
// current one (#3624): with zero prior edges, the repo-wide retract is a guaranteed no-op.
// A nil firstProjection or a row with no scope ID never skips, preserving the
// pre-#3624 behavior byte-identically. The probe result is memoized in memo
// (keyed by scope ID) so repeated refresh rows for the same scope within one
// planRepoWideRetractWork call cost at most one lookup.
func skipFirstProjectionRetract(
	ctx context.Context,
	domain string,
	row SharedProjectionIntentRow,
	firstProjection FirstProjectionLookup,
	memo map[string]bool,
	logger *slog.Logger,
) (bool, error) {
	if firstProjection == nil {
		return false, nil
	}
	scopeID := strings.TrimSpace(row.ScopeID)
	if scopeID == "" {
		return false, nil
	}

	hasPrior, memoized := memo[scopeID]
	if !memoized {
		var err error
		hasPrior, err = firstProjection.ScopeHasPriorGeneration(ctx, scopeID, row.GenerationID)
		if err != nil {
			return false, fmt.Errorf("check first projection for scope %s: %w", scopeID, err)
		}
		memo[scopeID] = hasPrior
	}
	if hasPrior {
		return false, nil
	}

	if logger != nil {
		logger.InfoContext(
			ctx,
			"skipped whole-scope retract on first projection",
			log.Domain(domain),
			slog.String("repo_id", sharedProjectionRowRepoID(row)),
			slog.String("scope_id", scopeID),
			slog.String("generation_id", row.GenerationID),
		)
	}
	return true, nil
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
	done, err := fence.HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
		ctx,
		key,
		row.GenerationID,
		refreshKey,
		domain,
	)
	if err != nil {
		return false, fmt.Errorf("check repo refresh fence for %s: %w", domain, err)
	}
	return done, nil
}
