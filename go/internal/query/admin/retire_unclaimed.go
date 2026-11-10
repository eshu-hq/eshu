// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ErrRetireUnclaimedUnknownInstance fails a never-claimed retirement when
// no collector_instances row names the (kind, instance) pair. The admin
// wrapper maps the workflow control store's gate refusal onto it so the
// handler stays on the admin port instead of the storage driver.
var ErrRetireUnclaimedUnknownInstance = errors.New("retire unclaimed workflow work items: unknown collector instance")

// ErrRetireUnclaimedInstanceActive fails a never-claimed retirement while
// the instance row is still enabled and not deactivated. The admin wrapper
// maps the workflow control store's gate refusal onto it.
var ErrRetireUnclaimedInstanceActive = errors.New("retire unclaimed workflow work items: collector instance still configured")

// RetiredUnclaimedItem is an admin-friendly view of one workflow work item
// the sanctioned never-claimed retirement transitioned to failed_terminal.
// Scope ids ride the response for the operator; they never become metric
// labels.
type RetiredUnclaimedItem struct {
	WorkItemID string `json:"work_item_id"`
	ScopeID    string `json:"scope_id"`
}

// retireUnclaimedWorkflowItems retires never-claimed pending workflow work
// items for one deconfigured collector instance: rows with status pending,
// attempt_count 0, and no current claim move to failed_terminal with the
// operator-retired failure class. The response count includes only rows
// transitioned by this call. An unknown instance fails closed with 404; a
// still-configured instance fails closed with 409 and a deny audit event,
// mirroring the skip's ambiguous-scope refusal. A 503 means the admin
// store is not configured on this API instance.
// POST /api/v0/admin/workflow-retire-unclaimed
func (h *Handler) retireUnclaimedWorkflowItems(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin store not configured")
		return
	}

	var req struct {
		CollectorKind       string `json:"collector_kind"`
		CollectorInstanceID string `json:"collector_instance_id"`
		OperatorNote        string `json:"operator_note"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	kind := strings.TrimSpace(req.CollectorKind)
	instanceID := strings.TrimSpace(req.CollectorInstanceID)
	if kind == "" || instanceID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "collector_kind and collector_instance_id are required and must not be empty")
		return
	}

	items, err := h.Store.RetireUnclaimedWorkflowItems(r.Context(), kind, instanceID, strings.TrimSpace(req.OperatorNote))
	if err != nil {
		if errors.Is(err, ErrRetireUnclaimedUnknownInstance) {
			querycontract.WriteError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, ErrRetireUnclaimedInstanceActive) {
			authCtx, _ := auth.AuthContextFromContext(r.Context())
			h.recordRecoveryAction(r.Context(), governanceaudit.DecisionDenied, "retire_unclaimed_refused_instance_active", authCtx, audit.SafeCorrelationID(audit.CorrelationID(r)))
			querycontract.WriteError(w, http.StatusConflict, err.Error())
			return
		}
		// Same shape as skip and dead-letter: a cancel can follow the
		// committed UPDATE, and a re-run touches only rows still pending
		// with no claim, so the mutation is state-idempotent.
		writeMutationFailure(w, r, err, retireUnclaimedFailedMessage)
		return
	}

	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"work_item_id": item.WorkItemID,
			"scope_id":     item.ScopeID,
		})
	}
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"count": len(items),
		"items": rows,
	})
}
