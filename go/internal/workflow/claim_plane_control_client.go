// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"time"
)

// Claim-plane control surface (Stage-1c, #8875).
//
// These six ControlStore methods travel the versioned workflow.v1 control
// routes served from the satellite's own database: run admission, work
// enqueue, instance reconciliation, completeness checkpoints, run
// reconciliation, and expired-claim reaping. The wire shapes below mirror
// the protocol table field-for-field but are declared locally, like the
// claim path in claim_plane_client.go: this package takes no dependency on
// the satellite module. No shared tables cross the boundary.
//
// Two ControlStore contracts need a client-side shim because the server
// rejects what the in-tree store accepts as a no-op: EnqueueWorkItems and
// UpsertCompletenessStates return nil without a round trip on an empty
// batch. The differential harness records both shims as known divergences.
type claimPlaneWireRun struct {
	RunID              string    `json:"run_id"`
	TriggerKind        string    `json:"trigger_kind"`
	Status             string    `json:"status"`
	RequestedScopeSet  string    `json:"requested_scope_set,omitempty"`
	RequestedCollector string    `json:"requested_collector,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	FinishedAt         time.Time `json:"finished_at,omitempty"`
}

func wireRun(run Run) claimPlaneWireRun {
	return claimPlaneWireRun{
		RunID: run.RunID, TriggerKind: string(run.TriggerKind), Status: string(run.Status),
		RequestedScopeSet: run.RequestedScopeSet, RequestedCollector: run.RequestedCollector,
		CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, FinishedAt: run.FinishedAt,
	}
}

func wireWorkItem(item WorkItem) claimPlaneWireItem {
	return claimPlaneWireItem{
		WorkItemID: item.WorkItemID, RunID: item.RunID,
		CollectorKind:       string(item.CollectorKind),
		CollectorInstanceID: item.CollectorInstanceID,
		SourceSystem:        item.SourceSystem, ScopeID: item.ScopeID,
		TenantID: item.TenantID, WorkspaceID: item.WorkspaceID,
		SubjectClass: item.SubjectClass, PolicyRevisionHash: item.PolicyRevisionHash,
		AcceptanceUnitID: item.AcceptanceUnitID, SourceRunID: item.SourceRunID,
		GenerationID: item.GenerationID, FairnessKey: item.FairnessKey,
		ClaimPriority: item.ClaimPriority, ClaimEcosystem: item.ClaimEcosystem,
		Status: string(item.Status), AttemptCount: item.AttemptCount,
		CurrentClaimID: item.CurrentClaimID, CurrentFencingToken: item.CurrentFencingToken,
		CurrentOwnerID: item.CurrentOwnerID, LeaseExpiresAt: item.LeaseExpiresAt,
		VisibleAt: item.VisibleAt, LastClaimedAt: item.LastClaimedAt,
		LastCompletedAt:  item.LastCompletedAt,
		LastFailureClass: item.LastFailureClass, LastFailureMessage: item.LastFailureMessage,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

// claimPlaneWireDesiredInstance mirrors one protocol desired instance.
type claimPlaneWireDesiredInstance struct {
	InstanceID    string `json:"instance_id"`
	CollectorKind string `json:"collector_kind"`
	Mode          string `json:"mode"`
	Enabled       bool   `json:"enabled"`
	Bootstrap     bool   `json:"bootstrap"`
	ClaimsEnabled bool   `json:"claims_enabled"`
	DisplayName   string `json:"display_name,omitempty"`
	Configuration string `json:"configuration,omitempty"`
}

func wireDesiredInstance(instance DesiredCollectorInstance) claimPlaneWireDesiredInstance {
	return claimPlaneWireDesiredInstance{
		InstanceID: instance.InstanceID, CollectorKind: string(instance.CollectorKind),
		Mode: string(instance.Mode), Enabled: instance.Enabled,
		Bootstrap: instance.Bootstrap, ClaimsEnabled: instance.ClaimsEnabled,
		DisplayName: instance.DisplayName, Configuration: instance.Configuration,
	}
}

// claimPlaneWireCompleteness mirrors one protocol completeness checkpoint.
type claimPlaneWireCompleteness struct {
	RunID         string    `json:"run_id"`
	CollectorKind string    `json:"collector_kind"`
	Keyspace      string    `json:"keyspace"`
	PhaseName     string    `json:"phase_name"`
	Required      bool      `json:"required"`
	Status        string    `json:"status"`
	Detail        string    `json:"detail,omitempty"`
	ObservedAt    time.Time `json:"observed_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func wireCompleteness(state CompletenessState) claimPlaneWireCompleteness {
	return claimPlaneWireCompleteness{
		RunID: state.RunID, CollectorKind: string(state.CollectorKind),
		Keyspace: string(state.Keyspace), PhaseName: state.PhaseName,
		Required: state.Required, Status: state.Status, Detail: state.Detail,
		ObservedAt: state.ObservedAt, UpdatedAt: state.UpdatedAt,
	}
}

// claimPlaneWireReapedClaim extends the claim wire shape with the failure
// detail the server reports on expired epochs.
type claimPlaneWireReapedClaim struct {
	claimPlaneWireClaim
	FailureClass   string `json:"failure_class,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
}

func wireReapedClaimToClaim(wire claimPlaneWireReapedClaim) Claim {
	claim := wireClaimToClaim(wire.claimPlaneWireClaim)
	claim.FailureClass = wire.FailureClass
	claim.FailureMessage = wire.FailureMessage
	return claim
}

// CreateRun admits one durable workflow run through the server. Admission
// is idempotent on run id: an ambiguous-timeout replay converges.
func (c *ClaimPlaneClient) CreateRun(ctx context.Context, run Run) error {
	var response struct {
		RunID string `json:"run_id"`
	}
	return c.post(ctx, "CreateRun", "/workflow.v1/CreateRun",
		map[string]any{"run": wireRun(run)}, &response)
}

// EnqueueWorkItems admits work items idempotently through the server. An
// empty batch is a no-op without a round trip, matching the in-tree store:
// the server rejects empty batches as invalid.
func (c *ClaimPlaneClient) EnqueueWorkItems(ctx context.Context, items []WorkItem) error {
	if len(items) == 0 {
		return nil
	}
	wire := make([]claimPlaneWireItem, 0, len(items))
	for _, item := range items {
		wire = append(wire, wireWorkItem(item))
	}
	var response struct {
		Accepted          int `json:"accepted"`
		InsertedWorkItems int `json:"inserted_work_items"`
	}
	return c.post(ctx, "EnqueueWorkItems", "/workflow.v1/EnqueueWorkItems",
		map[string]any{"items": wire}, &response)
}

// ReconcileCollectorInstances declares the desired collector instances
// through the server; absent instances deactivate.
func (c *ClaimPlaneClient) ReconcileCollectorInstances(
	ctx context.Context,
	observedAt time.Time,
	desired []DesiredCollectorInstance,
) error {
	wire := make([]claimPlaneWireDesiredInstance, 0, len(desired))
	for _, instance := range desired {
		wire = append(wire, wireDesiredInstance(instance))
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "ReconcileCollectorInstances",
		"/workflow.v1/ReconcileCollectorInstances", map[string]any{
			"observed_at": observedAt.UTC(),
			"instances":   wire,
		}, &ack)
}

// UpsertCompletenessStates records reducer-facing checkpoints through the
// server. An empty batch is a no-op without a round trip, matching the
// in-tree store: the server rejects empty batches as invalid.
func (c *ClaimPlaneClient) UpsertCompletenessStates(ctx context.Context, states []CompletenessState) error {
	if len(states) == 0 {
		return nil
	}
	wire := make([]claimPlaneWireCompleteness, 0, len(states))
	for _, state := range states {
		wire = append(wire, wireCompleteness(state))
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, "UpsertCompletenessStates",
		"/workflow.v1/UpsertCompletenessStates",
		map[string]any{"states": wire}, &ack)
}

// ReconcileWorkflowRuns advances drained runs through the server and
// reports how many transitioned.
func (c *ClaimPlaneClient) ReconcileWorkflowRuns(ctx context.Context, observedAt time.Time) (int, error) {
	var response struct {
		Transitioned int `json:"transitioned"`
	}
	if err := c.post(ctx, "ReconcileWorkflowRuns",
		"/workflow.v1/ReconcileWorkflowRuns",
		map[string]any{"observed_at": observedAt.UTC()}, &response); err != nil {
		return 0, err
	}
	return response.Transitioned, nil
}

// ReapExpiredClaims expires stale claims through the server. Pinned claims
// survive the server reaper; the in-tree store knows no pins, which the
// differential harness records as a known divergence.
func (c *ClaimPlaneClient) ReapExpiredClaims(
	ctx context.Context,
	observedAt time.Time,
	limit int,
	requeueDelay time.Duration,
) ([]Claim, error) {
	var response struct {
		Claims []claimPlaneWireReapedClaim `json:"claims,omitempty"`
	}
	if err := c.post(ctx, "ReapExpiredClaims",
		"/workflow.v1/ReapExpiredClaims", map[string]any{
			"observed_at":      observedAt.UTC(),
			"limit":            limit,
			"requeue_delay_ns": int64(requeueDelay),
		}, &response); err != nil {
		return nil, err
	}
	claims := make([]Claim, 0, len(response.Claims))
	for _, wire := range response.Claims {
		claims = append(claims, wireReapedClaimToClaim(wire))
	}
	return claims, nil
}
