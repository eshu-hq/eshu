// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Claim-plane wire shapes for the claim path (#9376).
//
// claimPlaneWireItem and claimPlaneWireClaim mirror the protocol JSON
// field-for-field, and the wire*To* converters below are the only path
// from those shapes into domain types. Closed enums validate at this
// boundary: a present but unknown value is a newer major speaking, so
// decode fails closed as invalid instead of silent success. Blank stays
// valid for oldest servers that omit fields, and open enums such as
// CollectorKind never pass through wireEnum.
//
// The pacing signal is the one advisory exception (#10377): it is only
// a reason label on an already-quantified directive, so wirePacingSignal
// widens an unknown value to the protective global default with the
// directive preserved and counted, instead of erroring. Do not "fix"
// it back to a hard error: that would starve claims on upgrade skew.

// claimPlaneWireItem mirrors the protocol work-item JSON. Durations travel
// as integer nanoseconds.
type claimPlaneWireItem struct {
	WorkItemID          string    `json:"work_item_id"`
	RunID               string    `json:"run_id"`
	CollectorKind       string    `json:"collector_kind"`
	CollectorInstanceID string    `json:"collector_instance_id"`
	SourceSystem        string    `json:"source_system"`
	ScopeID             string    `json:"scope_id"`
	TenantID            string    `json:"tenant_id,omitempty"`
	WorkspaceID         string    `json:"workspace_id,omitempty"`
	SubjectClass        string    `json:"subject_class,omitempty"`
	PolicyRevisionHash  string    `json:"policy_revision_hash,omitempty"`
	AcceptanceUnitID    string    `json:"acceptance_unit_id"`
	SourceRunID         string    `json:"source_run_id"`
	GenerationID        string    `json:"generation_id"`
	FairnessKey         string    `json:"fairness_key,omitempty"`
	ClaimPriority       int       `json:"claim_priority"`
	ClaimEcosystem      string    `json:"claim_ecosystem,omitempty"`
	Status              string    `json:"status"`
	AttemptCount        int       `json:"attempt_count"`
	CurrentClaimID      string    `json:"current_claim_id,omitempty"`
	CurrentFencingToken int64     `json:"current_fencing_token"`
	CurrentOwnerID      string    `json:"current_owner_id,omitempty"`
	LeaseExpiresAt      time.Time `json:"lease_expires_at,omitempty"`
	VisibleAt           time.Time `json:"visible_at,omitempty"`
	LastClaimedAt       time.Time `json:"last_claimed_at,omitempty"`
	LastCompletedAt     time.Time `json:"last_completed_at,omitempty"`
	LastFailureClass    string    `json:"last_failure_class,omitempty"`
	LastFailureMessage  string    `json:"last_failure_message,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// claimPlaneWireClaim mirrors the protocol claim JSON.
type claimPlaneWireClaim struct {
	ClaimID        string    `json:"claim_id"`
	WorkItemID     string    `json:"work_item_id"`
	FencingToken   int64     `json:"fencing_token"`
	OwnerID        string    `json:"owner_id"`
	Status         string    `json:"status"`
	ClaimedAt      time.Time `json:"claimed_at"`
	HeartbeatAt    time.Time `json:"heartbeat_at"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// wireEnum validates one decoded closed-enum value at the protocol
// boundary. Blank stays valid: an oldest server omits fields a newest
// server sends, and only a present but unknown value proves a newer major
// is speaking. Open enums such as CollectorKind never pass through here.
// The error reads as invalid, never as a fence rejection: callers must not
// drop the item and keep polling on a protocol mismatch.
func wireEnum[E interface {
	~string
	Validate() error
}](kind string, value E) error {
	if string(value) == "" {
		return nil
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("invalid %s %q: %w", kind, string(value), err)
	}
	return nil
}

// wirePacingSignal validates one decoded pacing signal at the protocol
// boundary (#10377). Blank and known values pass through untouched. A
// non-blank unknown value is a newer major speaking, but the pacing
// signal is only a reason label on an already-quantified directive, so
// the client widens it to the protective global default with the domain
// cleared instead of erroring: a hard decode error here would starve
// claims on the ClaimNext path until every client upgrades. The remap
// flag lets the caller count the event under a bounded outcome; the raw
// value is returned alongside so the ClaimNext path can keep it
// diagnosable without ever becoming a metric label.
func wirePacingSignal(raw string) (signal ClaimPacingSignal, remapped bool, unknown string) {
	if raw == "" {
		return "", false, ""
	}
	signal = ClaimPacingSignal(raw)
	if wireEnum("claim pacing signal", signal) == nil {
		return signal, false, ""
	}
	return ClaimPacingSignalGlobal, true, raw
}

func wireItemToWorkItem(wire claimPlaneWireItem) (WorkItem, error) {
	status := WorkItemStatus(wire.Status)
	if err := wireEnum("work-item status", status); err != nil {
		return WorkItem{}, err
	}
	return WorkItem{
		WorkItemID: wire.WorkItemID, RunID: wire.RunID,
		CollectorKind:       scope.CollectorKind(wire.CollectorKind),
		CollectorInstanceID: wire.CollectorInstanceID,
		SourceSystem:        wire.SourceSystem, ScopeID: wire.ScopeID,
		TenantID: wire.TenantID, WorkspaceID: wire.WorkspaceID,
		SubjectClass: wire.SubjectClass, PolicyRevisionHash: wire.PolicyRevisionHash,
		AcceptanceUnitID: wire.AcceptanceUnitID, SourceRunID: wire.SourceRunID,
		GenerationID: wire.GenerationID, FairnessKey: wire.FairnessKey,
		ClaimPriority: wire.ClaimPriority, ClaimEcosystem: wire.ClaimEcosystem,
		Status: status, AttemptCount: wire.AttemptCount,
		CurrentClaimID: wire.CurrentClaimID, CurrentFencingToken: wire.CurrentFencingToken,
		CurrentOwnerID: wire.CurrentOwnerID, LeaseExpiresAt: wire.LeaseExpiresAt,
		VisibleAt: wire.VisibleAt, LastClaimedAt: wire.LastClaimedAt,
		LastCompletedAt:  wire.LastCompletedAt,
		LastFailureClass: wire.LastFailureClass, LastFailureMessage: wire.LastFailureMessage,
		CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt,
	}, nil
}

func wireClaimToClaim(wire claimPlaneWireClaim) (Claim, error) {
	status := ClaimStatus(wire.Status)
	if err := wireEnum("claim status", status); err != nil {
		return Claim{}, err
	}
	return Claim{
		ClaimID: wire.ClaimID, WorkItemID: wire.WorkItemID,
		FencingToken: wire.FencingToken, OwnerID: wire.OwnerID,
		Status: status, ClaimedAt: wire.ClaimedAt,
		HeartbeatAt: wire.HeartbeatAt, LeaseExpiresAt: wire.LeaseExpiresAt,
		FinishedAt: wire.FinishedAt, CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt,
	}, nil
}
