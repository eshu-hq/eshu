// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"sort"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// refuseUnsafeExplicitReplay refuses an explicit work_item_ids replay that
// names replay-eligible items in an unsafe or manual-review failure class and
// was not forced. It reads before the idempotency claim so a refusal (or a read
// failure) never consumes the key, and it refuses the whole request so a mixed
// request replays nothing. It reports true when it wrote the response.
//
// Ids that do not exist, or that are not dead_letter/failed, produce no target
// and fall through to the replay unchanged, so a 200 with zero rows still means
// nothing matched.
func (h *Handler) refuseUnsafeExplicitReplay(
	w http.ResponseWriter,
	r *http.Request,
	req replayRequest,
	authCtx auth.AuthContext,
	correlationID string,
) bool {
	// A failure_class selector that reaches here is safe: an unsafe class was
	// already refused (or forced) by the selector guard in replay. "Unsafe AND
	// that class" is then empty by construction, so skip the read.
	if req.Force || len(req.WorkItemIDs) == 0 || req.FailureClass != "" {
		return false
	}
	targets, err := h.Store.UnsafeReplayTargets(r.Context(), UnsafeReplayTargetFilter{
		WorkItemIDs:          req.WorkItemIDs,
		ScopeID:              req.ScopeID,
		Stage:                req.Stage,
		FailureClass:         req.FailureClass,
		UnsafeFailureClasses: unsafeReplayFailureClassList(),
	})
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "replay unsafe-target check: "+err.Error())
		return true
	}
	if len(targets) == 0 {
		return false
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].WorkItemID < targets[j].WorkItemID })
	refused := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		guidance, _ := unsafeReplayRefusal(target.FailureClass)
		refused = append(refused, map[string]any{
			"work_item_id":  target.WorkItemID,
			"failure_class": target.FailureClass,
			"reason":        guidance,
		})
	}
	h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "replay_refused_unsafe_work_items", authCtx, correlationID)
	querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"status":             "refused",
		"reason":             "the named work items are in an unsafe or manual-review failure class; nothing was replayed",
		"detail":             "set force=true to replay these work items after addressing the cause",
		"refused_work_items": refused,
	})
	return true
}

// supersededGenerationReplayClass labels replay refusals for projector work
// whose generation is superseded. It matches the failure_class the recovery
// store records for the same fence (#7130).
const supersededGenerationReplayClass = "projector_replay_generation_superseded"

// refuseSupersededExplicitReplay refuses an explicit work_item_ids replay that
// names terminal projector rows whose scope generation is superseded. The
// store fences those rows out of every replay, so without this read the
// request would answer 200 with the named ids silently missing. force does
// not bypass it: a replayed row would re-project the retired generation's
// graph and content over the published one. Like the unsafe-class refusal it reads before the idempotency
// claim and refuses the whole request. It reports true when it wrote the
// response.
func (h *Handler) refuseSupersededExplicitReplay(
	w http.ResponseWriter,
	r *http.Request,
	req replayRequest,
	authCtx auth.AuthContext,
	correlationID string,
) bool {
	if len(req.WorkItemIDs) == 0 {
		return false
	}
	targets, err := h.Store.SupersededReplayTargets(r.Context(), UnsafeReplayTargetFilter{
		WorkItemIDs:  req.WorkItemIDs,
		ScopeID:      req.ScopeID,
		Stage:        req.Stage,
		FailureClass: req.FailureClass,
	})
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "replay superseded-generation check: "+err.Error())
		return true
	}
	if len(targets) == 0 {
		return false
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].WorkItemID < targets[j].WorkItemID })
	refused := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		refused = append(refused, map[string]any{
			"work_item_id":  target.WorkItemID,
			"generation_id": target.GenerationID,
			"failure_class": supersededGenerationReplayClass,
			"reason":        "the work item's generation is superseded by a newer ingestion of the same scope",
		})
	}
	if h.Instruments != nil && h.Instruments.SupersededGenerationFence != nil {
		h.Instruments.SupersededGenerationFence.Add(r.Context(), int64(len(targets)),
			metric.WithAttributes(telemetry.AttrFailureClass(supersededGenerationReplayClass)))
	}
	h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "replay_refused_superseded_generation", authCtx, correlationID)
	querycontract.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"status":             "refused",
		"reason":             "the named projector work items belong to superseded generations; nothing was replayed",
		"detail":             "remove these work items from the request; force does not apply because replaying them would re-project a retired generation's graph and content",
		"refused_work_items": refused,
	})
	return true
}
