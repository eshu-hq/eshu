// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// collectorReadinessTTL is the duration for which a computed readiness result
// is cached before the underlying StatusReader (an O(6.6M-fact) Postgres
// aggregation) is consulted again. The status page polls this endpoint on
// every render; caching amortises the expensive query over a ~30-second window
// without changing any data semantics — the query still computes accurate
// MAX(observed_at) and evidence staleness, it just runs at most once per TTL
// instead of once per request.
//
// The underlying query cost will also drop naturally once the #3451
// reducer/projection backlog drains the active-fact count, but the cache
// ensures the SLA is met regardless of that backlog state.
const collectorReadinessTTL = 30 * time.Second

// collectorReadinessCache is a simple, thread-safe in-memory cache for the
// collector-readiness response payload keyed by auth scope. Two slots are
// maintained — one for unscoped (admin) callers and one for scoped (tenant)
// callers — because the two produce different payloads: scoped callers receive
// family-level readiness with instance IDs redacted, whereas unscoped callers
// receive the full per-instance detail. Sharing a single slot across scopes
// would allow an admin-warmed entry to be served to a scoped caller, leaking
// per-instance identity across auth boundaries.
//
// Zero value is a valid, empty (both slots expired) cache.
type collectorReadinessCache struct {
	mu            sync.Mutex
	unscopedEntry collectorReadinessCacheEntry
	scopedEntry   collectorReadinessCacheEntry
}

// collectorReadinessCacheEntry holds one auth-scope slot of the cache.
type collectorReadinessCacheEntry struct {
	payload map[string]any
	expiry  time.Time
}

// collectorReadinessEntry is the redacted, console-consumable readiness record
// for one collector family or instance. It reuses the status promotion-proof
// vocabulary so the API and MCP surfaces speak the same readiness language and
// carries no credentials or raw provider payloads.
type collectorReadinessEntry struct {
	CollectorKind         string   `json:"collector_kind"`
	InstanceID            string   `json:"instance_id,omitempty"`
	DisplayName           string   `json:"display_name,omitempty"`
	PromotionState        string   `json:"promotion_state"`
	RuntimeCategory       string   `json:"runtime_category,omitempty"`
	Health                string   `json:"health,omitempty"`
	ClaimDriven           bool     `json:"claim_driven"`
	ClaimState            string   `json:"claim_state,omitempty"`
	SourceScope           string   `json:"source_scope,omitempty"`
	FixtureOnly           bool     `json:"fixture_only,omitempty"`
	EvidenceSources       []string `json:"evidence_sources,omitempty"`
	SourceSystems         []string `json:"source_systems,omitempty"`
	ObservationCount      int      `json:"observation_count,omitempty"`
	ReducerReadback       string   `json:"reducer_readback"`
	TelemetryHandles      []string `json:"telemetry_handles,omitempty"`
	Blockers              []string `json:"blockers,omitempty"`
	RecommendedNextAction string   `json:"recommended_next_action"`
	LastProofAt           any      `json:"last_proof_at,omitempty"`
	UpdatedAt             any      `json:"updated_at,omitempty"`
}

// getCollectorReadiness serves the per-collector-family promotion readiness read
// model over the full collector fleet. It loads the same status report the rest
// of the status surface uses and projects status.CollectorPromotionProofs so API
// and MCP callers see identical, redacted readiness truth.
//
// The response is cached for collectorReadinessTTL (~30 s) because the
// underlying StatusReader executes an O(total-active-facts) Postgres aggregation
// that takes several seconds at production scale. Caching amortises that cost
// over the TTL window; data semantics are unchanged — the query still computes
// accurate MAX(observed_at) and evidence staleness on each cache miss.
func (h *StatusHandler) getCollectorReadiness(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	// Determine the caller's auth scope first: scoped (tenant) callers receive
	// family-level readiness with instance IDs redacted; unscoped (admin) callers
	// receive full per-instance detail. The cache maintains one slot per scope so
	// an admin-warmed entry is never served to a scoped caller (and vice-versa).
	redactInstance := scopedAuthContext(r.Context())

	// Return cached payload when still within the TTL window for this scope.
	h.readinessCache.mu.Lock()
	entry := &h.readinessCache.unscopedEntry
	if redactInstance {
		entry = &h.readinessCache.scopedEntry
	}
	if time.Now().Before(entry.expiry) && entry.payload != nil {
		payload := entry.payload
		h.readinessCache.mu.Unlock()
		WriteSuccess(w, r, http.StatusOK, payload, &TruthEnvelope{
			Level: TruthLevelExact,
			Basis: TruthBasisRuntimeState,
			Freshness: TruthFreshness{
				State:      FreshnessFresh,
				ObservedAt: payload["generated_at"].(string),
			},
		})
		return
	}
	h.readinessCache.mu.Unlock()

	asOf := time.Now()
	report, err := status.LoadReport(r.Context(), h.StatusReader, asOf, status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	// Evaluate evidence staleness against the snapshot's own AsOf, not a freshly
	// captured wall clock. The text (RenderText) and status-JSON surfaces already
	// classify against report.AsOf; matching them here keeps the three readiness
	// surfaces in agreement and makes the verdict a function of the snapshot
	// rather than of when this handler happens to run. In production the reader
	// stamps RawSnapshot.AsOf with the request time, so this preserves live
	// behavior while removing the wall-clock divergence.
	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		AsOf:       report.AsOf,
		StaleAfter: status.DefaultCollectorPromotionStaleAfter,
	})
	entries := collectorReadinessEntries(proofs, redactInstance)

	payload := map[string]any{
		"version":      buildinfo.AppVersion(),
		"generated_at": asOf.UTC().Format(time.RFC3339),
		"readiness":    entries,
		"count":        len(entries),
	}

	// Store the freshly computed payload in the scope-specific cache slot.
	h.readinessCache.mu.Lock()
	entry = &h.readinessCache.unscopedEntry
	if redactInstance {
		entry = &h.readinessCache.scopedEntry
	}
	entry.payload = payload
	entry.expiry = time.Now().Add(collectorReadinessTTL)
	h.readinessCache.mu.Unlock()

	WriteSuccess(w, r, http.StatusOK, payload, &TruthEnvelope{
		Level: TruthLevelExact,
		Basis: TruthBasisRuntimeState,
		Freshness: TruthFreshness{
			State:      FreshnessFresh,
			ObservedAt: asOf.UTC().Format(time.RFC3339),
		},
	})
}

// collectorReadinessEntries projects promotion proofs into the readiness wire
// shape. When redactInstance is set (scoped callers) the per-instance identity is
// withheld so a scoped token sees family-level readiness only.
func collectorReadinessEntries(proofs []status.CollectorPromotionProof, redactInstance bool) []collectorReadinessEntry {
	entries := make([]collectorReadinessEntry, 0, len(proofs))
	for _, proof := range proofs {
		entry := collectorReadinessEntry{
			CollectorKind:         proof.CollectorKind,
			InstanceID:            proof.InstanceID,
			DisplayName:           proof.DisplayName,
			PromotionState:        proof.PromotionState,
			RuntimeCategory:       proof.RuntimeCategory,
			Health:                proof.Health,
			ClaimDriven:           proof.ClaimDriven,
			ClaimState:            proof.ClaimState,
			SourceScope:           proof.SourceScope,
			FixtureOnly:           proof.FixtureOnly,
			EvidenceSources:       proof.EvidenceSources,
			SourceSystems:         proof.SourceSystems,
			ObservationCount:      proof.ObservationCount,
			ReducerReadback:       proof.ReducerReadback,
			TelemetryHandles:      proof.TelemetryHandles,
			Blockers:              proof.Blockers,
			RecommendedNextAction: recommendedNextAction(proof),
			LastProofAt:           nullableRFC3339(proof.LastObservedAt),
			UpdatedAt:             nullableRFC3339(proof.UpdatedAt),
		}
		if redactInstance {
			entry.InstanceID = ""
		}
		entries = append(entries, entry)
	}
	return entries
}

// recommendedNextAction maps a promotion proof to the single most useful next
// step a reviewer or operator should take. It reuses the proof's own safe
// blocker text so no provider detail is invented.
func recommendedNextAction(proof status.CollectorPromotionProof) string {
	switch proof.PromotionState {
	case status.CollectorPromotionImplemented:
		return "No action required; lane meets the promotion contract."
	case status.CollectorPromotionPartial:
		if proof.ReducerReadback == status.CollectorReadbackPending {
			return "Confirm reducer readback; source facts exist but have not been admitted."
		}
		return withBlocker("Complete the promotion contract", proof.Blockers)
	case status.CollectorPromotionFailed:
		return withBlocker("Investigate the runtime failure", proof.Blockers)
	case status.CollectorPromotionStale:
		return "Re-run the collector; the newest evidence is older than the freshness window."
	case status.CollectorPromotionGated:
		return "Enable claims or remove the runtime profile gate to activate this lane."
	case status.CollectorPromotionDisabled:
		return "Enable the collector instance to resume collection."
	case status.CollectorPromotionPermissionHidden:
		return "Request access; this collector family is hidden by the active permission scope."
	case status.CollectorPromotionUnsupported:
		return "Configure a collector instance for this family to begin collection."
	default:
		return "Review the collector configuration."
	}
}

func withBlocker(prefix string, blockers []string) string {
	if len(blockers) > 0 {
		return prefix + ": " + blockers[0]
	}
	return prefix + "."
}
