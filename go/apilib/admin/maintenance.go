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
)

// reindexProgressFailedMessage is the fixed client-facing text for a failed
// reindex-progress step. A store error quotes SQL, hosts, and connection
// settings, so err.Error() never reaches the body.
const reindexProgressFailedMessage = "reindex progress query failed"

// maxReindexProgressScopes bounds one progress read to 100 scope IDs, like
// a repository-scoped request in core. maxReindexProgressSamples caps the
// per-scope detail; counts always cover every evaluated scope.
const (
	maxReindexProgressScopes  = 100
	maxReindexProgressSamples = 10
)

// reindexProgressIngester is the only ingester name the progress read asks
// its store for: the repository watermark every git ingester shard reads
// its reindex watermark from. It mirrors core's ReindexIngesterRepository
// without importing the runtime package.
const reindexProgressIngester = "repository"

// ReindexState is the fleet reindex watermark the progress read compares
// each scope against: the requested_at the fleet row carries, zero when no
// reindex was ever requested.
type ReindexState struct {
	RequestedAt time.Time
}

// FullReconcileState is the progress view of one scope's full generations:
// whether any full activated, the newest activated full's own ingest time
// (a re-projection Ack never moves it), and the active generation. It is
// the minimal shape the progress classification reads, so a reader-handle
// store can satisfy it without the writer types.
type FullReconcileState struct {
	HasProjectedFull   bool
	LastStoredFullAt   time.Time
	ActiveGenerationID string
}

// MaintenanceStore is the minimal port the reindex-progress read runs
// through: the fleet watermark, the named scopes' per-repository
// watermarks, and each scope's full-generation state. Every method only
// reads; the endpoint records nothing, claims nothing, and completes
// nothing. A reader-handle store satisfies it, so serving this read needs
// no writer credential. The full ReadStore does not include it.
type MaintenanceStore interface {
	GetReindexState(ctx context.Context, ingester string) (ReindexState, error)
	RepositoryReindexRequestedAt(ctx context.Context, scopeIDs []string) (map[string]time.Time, error)
	FullReconcileState(ctx context.Context, scopeID string) (FullReconcileState, error)
}

// ServeReindexProgress answers the fleet reindex-progress read
// (GET /api/v0/admin/reindex/progress?scope_id=...&fleet_requested_at=...):
// nil store 503s, a missing/blank/over-cap scope_id selection or a
// malformed fleet_requested_at override 400s with the core-identical
// detail, and success answers the progress report with core field and
// timestamp parity (complete only when no evaluated scope is pending or
// never-seen, unfinished scopes first, at most 10 samples). A store failure
// first offers onStoreError the fence verdicts (nil skips); then a client
// cancel answers 499, anything else the fixed 500.
func ServeReindexProgress(w http.ResponseWriter, r *http.Request, store MaintenanceStore, onStoreError StoreErrorHandler) {
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	scopeIDs, err := decodeProgressScopeIDs(r.URL.Query()["scope_id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fleet, overridden, err := decodeProgressFleet(r.URL.Query().Get("fleet_requested_at"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !overridden {
		state, err := store.GetReindexState(r.Context(), reindexProgressIngester)
		if err != nil {
			writeProgressFailure(w, r, err, onStoreError)
			return
		}
		fleet = state.RequestedAt
	}
	perRepo, err := store.RepositoryReindexRequestedAt(r.Context(), scopeIDs)
	if err != nil {
		writeProgressFailure(w, r, err, onStoreError)
		return
	}
	evaluated := make([]reindexProgressScope, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		state, err := store.FullReconcileState(r.Context(), scopeID)
		if err != nil {
			writeProgressFailure(w, r, err, onStoreError)
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
	writeReindexProgressReport(w, fleet, evaluated)
}

// writeProgressFailure answers a failed progress step: the fence hook
// first, then a client cancel with 499, otherwise the fixed 500.
func writeProgressFailure(w http.ResponseWriter, r *http.Request, err error, onStoreError StoreErrorHandler) {
	if onStoreError != nil && onStoreError(w, r, err) {
		return
	}
	if clientCanceled(r.Context(), err) {
		writeError(w, clientClosedStatus, reindexProgressFailedMessage)
		return
	}
	writeError(w, http.StatusInternalServerError, reindexProgressFailedMessage)
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
// GREATEST semantics: a per-repository row at or before the fleet watermark
// is already subsumed by it.
func progressEffectiveWatermark(fleet time.Time, perRepo map[string]time.Time, scopeID string) time.Time {
	if requested, ok := perRepo[scopeID]; ok && requested.After(fleet) {
		return requested
	}
	return fleet
}

// progressScopeState classifies one scope against its effective watermark:
// a scope with no activated full is never-seen, a stored full at or after
// the watermark is satisfied, anything older is pending. Only the newest
// activated full's own ingest time answers the watermark: a re-projection
// Ack moves the projected time but never the stored time, so it must never
// satisfy a scope here.
func progressScopeState(watermark time.Time, state FullReconcileState) string {
	if !state.HasProjectedFull {
		return "never_seen"
	}
	if !state.LastStoredFullAt.Before(watermark) {
		return "satisfied"
	}
	return "pending"
}

// decodeProgressScopeIDs validates the repeated scope_id parameter: 1 to
// 100 scope IDs, none blank, deduplicated and sorted so reads and samples
// are deterministic.
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
	if len(scopeIDs) == 0 || len(scopeIDs) > maxReindexProgressScopes {
		return nil, fmt.Errorf("scope_id requires 1 to %d scope ids, got %d", maxReindexProgressScopes, len(scopeIDs))
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

// writeReindexProgressReport renders the progress report: complete only
// when no evaluated scope is pending or never-seen, the exact watermarks
// compared, counts over every evaluated scope, and at most 10 scope
// samples — unfinished scopes first, each group by scope ID — with
// truncated set when scopes were left out. scopes is always an array,
// never null.
func writeReindexProgressReport(w http.ResponseWriter, fleet time.Time, evaluated []reindexProgressScope) {
	counts := map[string]any{
		"satisfied":  0,
		"pending":    0,
		"never_seen": 0,
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
			"requested_at":         progressReportTime(entry.effective),
			"last_stored_full_at":  progressReportStoredFull(entry),
			"active_generation_id": entry.activeGenerationID,
		})
	}
	status := "complete"
	if counts["pending"].(int) > 0 || counts["never_seen"].(int) > 0 {
		status = "incomplete"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             status,
		"fleet_requested_at": progressReportTime(fleet),
		"counts":             counts,
		"scopes":             samples,
		"truncated":          len(evaluated) > len(samples),
	})
}

// progressSampleRank orders unfinished scopes before satisfied ones so the
// samples answer what still blocks completion first.
func progressSampleRank(state string) int {
	switch state {
	case "pending":
		return 0
	case "never_seen":
		return 1
	default:
		return 2
	}
}

// progressReportTime renders a watermark in UTC, or null when no watermark
// was ever recorded.
func progressReportTime(watermark time.Time) any {
	if watermark.IsZero() {
		return nil
	}
	return watermark.UTC().Format(time.RFC3339Nano)
}

// progressReportStoredFull renders the scope's newest activated full ingest
// time, or null when the scope has no activated full.
func progressReportStoredFull(entry reindexProgressScope) any {
	if !entry.hasStoredFull {
		return nil
	}
	return entry.lastStoredFullAt.UTC().Format(time.RFC3339Nano)
}
