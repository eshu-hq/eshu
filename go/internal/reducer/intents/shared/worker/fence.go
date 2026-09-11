// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// RowUsesRefreshFence reports whether a per-edge row opted into the repo-wide
// retract fence by carrying the retract_via_refresh marker its paired refresh
// intent guarantees. Rows without it predate #2898 emission and stay on the
// legacy per-partition retract path.
func RowUsesRefreshFence(row sharedintent.Row) bool {
	return payloadcore.PayloadBool(row.Payload, sharedintent.RetractViaRefreshKey)
}

// RefreshFenceLookup reports whether a repo's whole-scope
// refresh partition has completed for the current generation. It is the durable
// happens-before signal that lets a per-edge upsert row write only after the
// single repo-wide retract for its generation has committed, even when
// partitions are processed concurrently across workers or replicas (#2898,
// #5554). Exact same-generation redelivery is idempotent because intent IDs are
// deterministic and completed rows are not reopened by the durable upsert.
type RefreshFenceLookup interface {
	HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
		ctx context.Context,
		key sharedintent.AcceptanceKey,
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

// RepoWideRetractPlan is the split of a repo-wide-retract domain's selected batch
// into the rows that retract, the rows that write, the rows to mark completed,
// and the count of per-edge rows held by the refresh fence this cycle.
type RepoWideRetractPlan struct {
	RetractRows   []sharedintent.Row
	WriteRows     []sharedintent.Row
	CompletedRows []sharedintent.Row
	Deferred      int
}

// PlanRepoWideRetractWork splits a repo-wide-retract domain's ready rows so the
// repo-wide retract is issued only by the per-repo refresh intent, and per-edge
// rows write only once that refresh has retracted (#2898/#2910). It is called
// only when a fence lookup is wired and sharedintent.DomainHasRepoWideRetract(domain) is true.
//
// Within one partition cycle a refresh row retracts (repo-wide) before any write
// happens, so per-edge rows for a repo whose refresh is in this same batch are
// safe to write now. Per-edge rows whose refresh lives in another partition are
// written only after the durable fence reports that refresh completed; otherwise
// they are deferred (left pending, not written, not completed) and re-selected
// next cycle. A refresh row never writes (sharedintent.FilterUpsertRows drops it).
//
// firstProjection additionally lets a refresh row skip its whole-scope retract
// entirely (#3624): when the row's scope has no generation other than the
// current one, this is the scope's first projection, so there are zero prior edges and the retract
// is a guaranteed no-op. The row still lands in plan.CompletedRows so the fence
// opens and per-edge writes proceed; only the (expensive, full-scan-on-NornicDB)
// retract call is skipped. A nil firstProjection disables the skip, leaving the
// retract byte-identical to prior behavior. The probe is memoized per scope ID
// within one call so a batch with many refresh rows for the same scope costs at
// most one lookup. logger is optional; when set, a skip is logged as an operator
// signal.
func PlanRepoWideRetractWork(
	ctx context.Context,
	domain string,
	rows []sharedintent.Row,
	fence RefreshFenceLookup,
	firstProjection FirstProjectionLookup,
	logger *slog.Logger,
) (RepoWideRetractPlan, error) {
	plan := RepoWideRetractPlan{}
	refreshReposInBatch := make(map[string]struct{})
	for _, row := range rows {
		if sharedintent.IsRepoRefreshRow(row) {
			refreshReposInBatch[sharedintent.RowRepoID(row)] = struct{}{}
		}
	}

	firstProjectionMemo := make(map[string]bool)

	for _, row := range rows {
		if sharedintent.IsRepoRefreshRow(row) {
			plan.CompletedRows = append(plan.CompletedRows, row)
			skip, err := skipFirstProjectionRetract(ctx, domain, row, firstProjection, firstProjectionMemo, logger)
			if err != nil {
				return RepoWideRetractPlan{}, err
			}
			if !skip {
				plan.RetractRows = append(plan.RetractRows, row)
			}
			continue
		}

		if !RowUsesRefreshFence(row) {
			// Legacy in-flight row (no paired refresh): keep the pre-#2898
			// per-partition retract so it drains instead of deferring forever. It is
			// superseded by the next re-ingest's fenced, marked rows.
			plan.RetractRows = append(plan.RetractRows, row)
			plan.WriteRows = append(plan.WriteRows, row)
			plan.CompletedRows = append(plan.CompletedRows, row)
			continue
		}

		repoID := sharedintent.RowRepoID(row)
		if _, refreshHere := refreshReposInBatch[repoID]; refreshHere {
			// The refresh for this repo retracts earlier in this same cycle, so the
			// write is already ordered after it.
			plan.WriteRows = append(plan.WriteRows, row)
			plan.CompletedRows = append(plan.CompletedRows, row)
			continue
		}

		ready, err := perEdgeRowReady(ctx, domain, row, fence)
		if err != nil {
			return RepoWideRetractPlan{}, err
		}
		if !ready {
			plan.Deferred++
			continue
		}
		plan.WriteRows = append(plan.WriteRows, row)
		plan.CompletedRows = append(plan.CompletedRows, row)
	}

	return plan, nil
}

// skipFirstProjectionRetract reports whether a refresh row's whole-scope
// retract may be skipped because the scope has no generation other than the
// current one (#3624): with zero prior edges, the repo-wide retract is a guaranteed no-op.
// A nil firstProjection or a row with no scope ID never skips, preserving the
// pre-#3624 behavior byte-identically. The probe result is memoized in memo
// (keyed by scope ID) so repeated refresh rows for the same scope within one
// PlanRepoWideRetractWork call cost at most one lookup.
func skipFirstProjectionRetract(
	ctx context.Context,
	domain string,
	row sharedintent.Row,
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
			slog.String("repo_id", sharedintent.RowRepoID(row)),
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
	row sharedintent.Row,
	fence RefreshFenceLookup,
) (bool, error) {
	key, ok := row.AcceptanceKey()
	if !ok {
		return true, nil
	}
	refreshKey := sharedintent.RepoWideRetractRefreshPartitionKey(domain, sharedintent.RowRepoID(row))
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
