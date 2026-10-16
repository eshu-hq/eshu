// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Reindex progress states (#7620). A scope is satisfied when its newest
// activated full generation was ingested at or after its effective
// watermark, pending when it has an activated full older than that, and
// never_seen when it has no activated full at all. There are no sub-codes
// in v1: pending never says why the re-parse has not landed yet.
const (
	reindexProgressSatisfied = "satisfied"
	reindexProgressPending   = "pending"
	reindexProgressNeverSeen = "never_seen"
)

// maxReindexProgressSamples caps the per-scope detail of the progress
// response. Counts always cover every evaluated scope; scopes lists at most
// this many samples with truncated set when scopes were left out.
const maxReindexProgressSamples = 10

// ReindexProgressReader serves the read-only inputs of
// GET /api/v0/admin/reindex/progress: the fleet reindex watermark, the
// named scopes' per-repository watermarks, and each scope's newest
// activated full generation state. Every method only reads; the endpoint
// records nothing, claims nothing, and completes nothing.
type ReindexProgressReader interface {
	GetReindexState(ctx context.Context, ingester string) (runtime.ReindexRequest, error)
	RepositoryReindexRequestedAt(ctx context.Context, scopeIDs []string) (map[string]time.Time, error)
	FullReconcileState(ctx context.Context, scopeID string) (scope.FullReconcileState, error)
}

// reindexProgressScope is one scope's evaluated progress: the state the
// collector rule assigns it and the exact watermarks compared.
type reindexProgressScope struct {
	scopeID            string
	state              string
	effective          time.Time
	lastStoredFullAt   time.Time
	hasStoredFull      bool
	activeGenerationID string
}

// progressEffectiveWatermark returns the watermark scopeID is compared
// against: the later of the fleet watermark and the scope's own
// per-repository row, zero when neither is set. It mirrors the collector's
// reindexWatermarkFor (selection_baseline.go), including its GREATEST
// semantics: a per-repository row at or before the fleet watermark is
// already subsumed by it, so the fleet watermark is the value compared.
func progressEffectiveWatermark(fleet time.Time, perRepo map[string]time.Time, scopeID string) time.Time {
	if requested, ok := perRepo[scopeID]; ok && requested.After(fleet) {
		return requested
	}
	return fleet
}

// progressScopeState classifies one scope against its effective watermark.
// It mirrors the collector's reindexSatisfied
// (selection_reconcile_config.go): only the newest activated full
// generation's own ingest time (LastStoredFullAt) answers the watermark. A
// #7978 re-projection Ack moves LastProjectedFullAt but never
// LastStoredFullAt, so it must never satisfy a scope here either.
func progressScopeState(watermark time.Time, state scope.FullReconcileState) string {
	if !state.HasProjectedFull {
		return reindexProgressNeverSeen
	}
	if !state.LastStoredFullAt.Before(watermark) {
		return reindexProgressSatisfied
	}
	return reindexProgressPending
}

// reindexProgress answers how far a reindex watermark has propagated.
// GET /api/v0/admin/reindex/progress?scope_id=...&fleet_requested_at=...
func (h *Handler) reindexProgress(w http.ResponseWriter, r *http.Request) {
	if h.ReindexProgress == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "reindex progress handler not configured")
		return
	}
	scopeIDs, err := decodeProgressScopeIDs(r.URL.Query()["scope_id"])
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	fleet, overridden, err := decodeProgressFleet(r.URL.Query().Get("fleet_requested_at"))
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !overridden {
		state, err := h.ReindexProgress.GetReindexState(r.Context(), runtime.ReindexIngesterRepository)
		if err != nil {
			// A read step on the writer pool, like the reopen resolve:
			// no guarded-reader verdict applies, so a fault answers
			// the fixed message (499 when the caller canceled).
			writeMutationFailure(w, r, err, reindexProgressFailedMessage)
			return
		}
		fleet = state.RequestedAt
	}
	perRepo, err := h.ReindexProgress.RepositoryReindexRequestedAt(r.Context(), scopeIDs)
	if err != nil {
		writeMutationFailure(w, r, err, reindexProgressFailedMessage)
		return
	}
	evaluated := make([]reindexProgressScope, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		state, err := h.ReindexProgress.FullReconcileState(r.Context(), scopeID)
		if err != nil {
			writeMutationFailure(w, r, err, reindexProgressFailedMessage)
			return
		}
		effective := progressEffectiveWatermark(fleet, perRepo, scopeID)
		evaluated = append(evaluated, reindexProgressScope{
			scopeID:            scopeID,
			state:              progressScopeState(effective, state),
			effective:          effective,
			lastStoredFullAt:   state.LastStoredFullAt,
			hasStoredFull:      state.HasProjectedFull,
			activeGenerationID: state.ActiveGenerationID,
		})
	}
	writeReindexProgress(w, fleet.UTC(), evaluated)
}

// decodeProgressScopeIDs validates the repeated scope_id parameter: 1 to
// maxReindexRepositories scope IDs like a repository-scoped request, none
// blank, deduplicated and sorted so reads and samples are deterministic.
func decodeProgressScopeIDs(raw []string) ([]string, error) {
	trimmed := make([]string, 0, len(raw))
	for _, scopeID := range raw {
		if strings.TrimSpace(scopeID) == "" {
			return nil, fmt.Errorf("scope_id must not be blank")
		}
		trimmed = append(trimmed, strings.TrimSpace(scopeID))
	}
	slices.Sort(trimmed)
	scopeIDs := slices.Compact(trimmed)
	if len(scopeIDs) == 0 || len(scopeIDs) > maxReindexRepositories {
		return nil, fmt.Errorf("scope_id requires 1 to %d scope ids, got %d", maxReindexRepositories, len(scopeIDs))
	}
	return scopeIDs, nil
}

// decodeProgressFleet parses the optional fleet_requested_at override. It
// returns the override and true when present, or the zero time and false
// when the caller wants the live fleet watermark instead.
func decodeProgressFleet(raw string) (time.Time, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false, nil
	}
	requestedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("fleet_requested_at must be RFC 3339: %q", strings.TrimSpace(raw))
	}
	return requestedAt.UTC(), true, nil
}

// writeReindexProgress renders the progress report: complete only when no
// evaluated scope is pending or never-seen, the exact watermarks compared,
// counts over every evaluated scope, and at most
// maxReindexProgressSamples scope samples — unfinished scopes first, each
// group by scope ID — with truncated set when scopes were left out. scopes
// is always an array, never null.
func writeReindexProgress(w http.ResponseWriter, fleet time.Time, evaluated []reindexProgressScope) {
	counts := map[string]any{
		reindexProgressSatisfied: 0,
		reindexProgressPending:   0,
		reindexProgressNeverSeen: 0,
	}
	for _, entry := range evaluated {
		counts[entry.state] = counts[entry.state].(int) + 1
	}
	ordered := slices.Clone(evaluated)
	slices.SortFunc(ordered, func(a, b reindexProgressScope) int {
		if progressSampleRank(a.state) != progressSampleRank(b.state) {
			return progressSampleRank(a.state) - progressSampleRank(b.state)
		}
		return strings.Compare(a.scopeID, b.scopeID)
	})
	samples := make([]map[string]any, 0, maxReindexProgressSamples)
	for _, entry := range ordered {
		if len(samples) >= maxReindexProgressSamples {
			break
		}
		samples = append(samples, map[string]any{
			"scope_id":             entry.scopeID,
			"state":                entry.state,
			"requested_at":         progressTime(entry.effective),
			"last_stored_full_at":  progressStoredFull(entry),
			"active_generation_id": entry.activeGenerationID,
		})
	}
	status := "complete"
	if counts[reindexProgressPending].(int) > 0 || counts[reindexProgressNeverSeen].(int) > 0 {
		status = "incomplete"
	}
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"status":             status,
		"fleet_requested_at": progressTime(fleet),
		"counts":             counts,
		"scopes":             samples,
		"truncated":          len(evaluated) > len(samples),
	})
}

// progressSampleRank orders unfinished scopes before satisfied ones so the
// samples answer what still blocks completion first.
func progressSampleRank(state string) int {
	switch state {
	case reindexProgressPending:
		return 0
	case reindexProgressNeverSeen:
		return 1
	default:
		return 2
	}
}

// progressTime renders a watermark in UTC, or null when no watermark was
// ever recorded.
func progressTime(watermark time.Time) any {
	if watermark.IsZero() {
		return nil
	}
	return watermark.UTC().Format(time.RFC3339Nano)
}

// progressStoredFull renders the scope's newest activated full ingest time,
// or null when the scope has no activated full.
func progressStoredFull(entry reindexProgressScope) any {
	if !entry.hasStoredFull {
		return nil
	}
	return entry.lastStoredFullAt.UTC().Format(time.RFC3339Nano)
}
