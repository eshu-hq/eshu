package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

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
func (h *StatusHandler) getCollectorReadiness(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	asOf := time.Now()
	report, err := status.LoadReport(r.Context(), h.StatusReader, asOf, status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	proofs := status.CollectorPromotionProofs(report, status.CollectorPromotionOptions{
		AsOf:       asOf,
		StaleAfter: status.DefaultCollectorPromotionStaleAfter,
	})
	redactInstance := scopedAuthContext(r.Context())
	entries := collectorReadinessEntries(proofs, redactInstance)

	payload := map[string]any{
		"version":      buildinfo.AppVersion(),
		"generated_at": asOf.UTC().Format(time.RFC3339),
		"readiness":    entries,
		"count":        len(entries),
	}
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
