package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

const answerNarrationStatusCapability = "answer_narration.status"

func (h *StatusHandler) getAnswerNarrationStatus(w http.ResponseWriter, r *http.Request) {
	// When a governed posture func is wired, use it directly. This avoids a
	// round-trip through the status DB whose AnswerNarration field is a static
	// placeholder, and ensures the endpoint reflects real runtime gate state.
	if h != nil && h.NarrationPosture != nil {
		answerNarration := h.NarrationPosture()
		WriteSuccess(
			w,
			r,
			http.StatusOK,
			answerNarrationStatusToMap(answerNarration),
			answerNarrationStatusTruth(h.profile(), answerNarration),
		)
		return
	}

	// Fallback: load from status DB (returns DefaultAnswerNarrationStatus when
	// no reader is configured or when NarrationPosture is nil).
	report := status.BuildReport(status.RawSnapshot{}, status.DefaultOptions())
	if h != nil && h.StatusReader != nil {
		loaded, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
			return
		}
		report = loaded
	}

	answerNarration := report.AnswerNarration
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		answerNarrationStatusToMap(answerNarration),
		answerNarrationStatusTruth(h.profile(), answerNarration),
	)
}

func answerNarrationStatusToMap(snapshot status.AnswerNarrationStatus) map[string]any {
	normalized := status.BuildReport(
		status.RawSnapshot{AnswerNarration: snapshot},
		status.DefaultOptions(),
	).AnswerNarration
	out := map[string]any{
		"state":                            normalized.State,
		"reason":                           normalized.Reason,
		"provider_configured":              normalized.ProviderConfigured,
		"provider_traffic_enabled":         normalized.ProviderTrafficEnabled,
		"policy_allowed":                   normalized.PolicyAllowed,
		"budget_available":                 normalized.BudgetAvailable,
		"publish_safety_enabled":           normalized.PublishSafetyEnabled,
		"deterministic_fallback_available": normalized.DeterministicFallbackAvailable,
		"canonical_truth_affected":         normalized.CanonicalTruthAffected,
		"retention_posture":                normalized.RetentionPosture,
		"supported_states":                 status.AnswerNarrationSupportedStates(),
		"supported_reasons":                status.AnswerNarrationSupportedReasons(),
		"validator_reason_codes":           status.AnswerNarrationValidatorReasonCodes(),
	}
	if normalized.Detail != "" {
		out["detail"] = normalized.Detail
	}
	if normalized.PolicyHash != "" {
		out["policy_hash"] = normalized.PolicyHash
	}
	if !normalized.UpdatedAt.IsZero() {
		out["updated_at"] = normalized.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func answerNarrationStatusTruth(profile QueryProfile, snapshot status.AnswerNarrationStatus) *TruthEnvelope {
	truth := BuildTruthEnvelope(
		profile,
		answerNarrationStatusCapability,
		TruthBasisRuntimeState,
		"resolved from redacted answer narration runtime status",
	)
	if snapshot.State != status.AnswerNarrationAvailable {
		truth.Level = TruthLevelFallback
		truth.Freshness = TruthFreshness{
			State:  FreshnessUnavailable,
			Detail: snapshot.Reason,
		}
	}
	return truth
}
