// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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
	if req.Force || len(req.WorkItemIDs) == 0 {
		return false
	}
	targets, err := h.Store.UnsafeReplayTargets(r.Context(), UnsafeReplayTargetFilter{
		WorkItemIDs:          req.WorkItemIDs,
		ScopeID:              req.ScopeID,
		Stage:                req.Stage,
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
