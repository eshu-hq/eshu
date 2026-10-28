// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Claim-plane client seam (Stage-1b, #8875).
//
// ClaimPlaneClient implements ControlStore over the versioned workflow.v1
// HTTP protocol served by eshu-workflow, so core can cut over from the
// in-tree Postgres store without changing callers. The wire shapes below
// mirror the protocol table field-for-field but are declared locally: this
// package takes no dependency on the satellite module, and the client is
// purely additive — no existing constructor or behavior changes.
//
// Coverage: the claim path (ClaimNextEligible, Heartbeat, Complete,
// Release, Fail x2, Pin) plus the read probe ListCollectorInstances and
// the pacing consult CheckClaimPacing travel over HTTP. Run admission,
// reconciliation, completeness upserts, and the reaper stay server-internal
// by protocol ruling and report ErrClaimPlaneUnsupported until their
// Stage-1c routes exist. Nothing in-tree constructs this client yet.
type ClaimPlaneClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewClaimPlaneClient dials the claim-plane server at baseURL (scheme and
// host, no trailing path). A nil httpClient uses http.DefaultClient.
func NewClaimPlaneClient(baseURL string, httpClient *http.Client) *ClaimPlaneClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ClaimPlaneClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

// ErrClaimPlaneUnsupported reports a ControlStore method with no protocol
// route: server-internal by ruling, awaiting its Stage-1c route.
var ErrClaimPlaneUnsupported = fmt.Errorf("claim-plane protocol: operation stays server-internal")

// ClaimPacedError reports a claim refused by server-side pacing. It carries
// the backoff the runner must wait instead of treating the refusal as a
// failure.
type ClaimPacedError struct {
	Signal     ClaimPacingSignal
	Domain     string
	RetryAfter time.Duration
	Stuck      bool
}

// Error implements error.
func (e *ClaimPacedError) Error() string {
	return fmt.Sprintf("claim pacing engaged on %s depth (domain %q): grant delayed %s",
		e.Signal, e.Domain, e.RetryAfter)
}

// claimPlaneEnvelope is the protocol error body.
type claimPlaneEnvelope struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int64  `json:"retry_after_seconds,omitempty"`
}

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

func wireItemToWorkItem(wire claimPlaneWireItem) WorkItem {
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
		Status: WorkItemStatus(wire.Status), AttemptCount: wire.AttemptCount,
		CurrentClaimID: wire.CurrentClaimID, CurrentFencingToken: wire.CurrentFencingToken,
		CurrentOwnerID: wire.CurrentOwnerID, LeaseExpiresAt: wire.LeaseExpiresAt,
		VisibleAt: wire.VisibleAt, LastClaimedAt: wire.LastClaimedAt,
		LastCompletedAt:  wire.LastCompletedAt,
		LastFailureClass: wire.LastFailureClass, LastFailureMessage: wire.LastFailureMessage,
		CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt,
	}
}

func wireClaimToClaim(wire claimPlaneWireClaim) Claim {
	return Claim{
		ClaimID: wire.ClaimID, WorkItemID: wire.WorkItemID,
		FencingToken: wire.FencingToken, OwnerID: wire.OwnerID,
		Status: ClaimStatus(wire.Status), ClaimedAt: wire.ClaimedAt,
		HeartbeatAt: wire.HeartbeatAt, LeaseExpiresAt: wire.LeaseExpiresAt,
		FinishedAt: wire.FinishedAt, CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt,
	}
}

// post encodes body as JSON, posts it to the operation path with the
// protocol version header, and decodes a 200 into out. A protocol error
// status decodes the envelope and maps it to a caller error.
func (c *ClaimPlaneClient) post(
	ctx context.Context,
	operation, path string,
	body, out any,
) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("claim-plane %s: marshal: %w", operation, err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("claim-plane %s: request: %w", operation, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Workflow-Protocol-Version", "1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("claim-plane %s: do: %w", operation, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("claim-plane %s: read: %w", operation, err)
	}
	if response.StatusCode != http.StatusOK {
		var envelope claimPlaneEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("claim-plane %s: status %d (undecodable body)", operation, response.StatusCode)
		}
		return claimPlaneMapError(operation, envelope)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("claim-plane %s: decode: %w", operation, err)
	}
	return nil
}

// claimPlaneMapError maps a protocol envelope to a caller error.
// Fenced-away epochs collapse to ErrClaimRejected so callers keep the
// in-tree contract: drop the stale item and keep polling.
func claimPlaneMapError(operation string, envelope claimPlaneEnvelope) error {
	switch envelope.Code {
	case "stale_fence", "lease_expired", "claim_conflict":
		return fmt.Errorf("claim-plane %s: %w: %s", operation, ErrClaimRejected, envelope.Message)
	case "not_found":
		return fmt.Errorf("claim-plane %s: not found: %s", operation, envelope.Message)
	case "paced":
		return fmt.Errorf("claim-plane %s: paced: %s", operation, envelope.Message)
	case "invalid", "version_mismatch":
		return fmt.Errorf("claim-plane %s: invalid: %s", operation, envelope.Message)
	default:
		return fmt.Errorf("claim-plane %s: server error: %s", operation, envelope.Message)
	}
}

// claimPlaneMutation is the fenced mutation argument wire shape.
type claimPlaneMutation struct {
	WorkItemID               string    `json:"work_item_id"`
	ClaimID                  string    `json:"claim_id"`
	FencingToken             int64     `json:"fencing_token"`
	OwnerID                  string    `json:"owner_id"`
	ObservedAt               time.Time `json:"observed_at"`
	LeaseDuration            int64     `json:"lease_duration_ns"`
	TenantID                 string    `json:"tenant_id,omitempty"`
	WorkspaceID              string    `json:"workspace_id,omitempty"`
	SubjectClass             string    `json:"subject_class,omitempty"`
	PolicyRevisionHash       string    `json:"policy_revision_hash,omitempty"`
	FailureClass             string    `json:"failure_class,omitempty"`
	FailureMessage           string    `json:"failure_message,omitempty"`
	FailureOperation         string    `json:"failure_operation,omitempty"`
	FailureStatusCode        int       `json:"failure_status_code,omitempty"`
	FailureTerminal          bool      `json:"failure_terminal,omitempty"`
	VisibleAt                time.Time `json:"visible_at,omitempty"`
	ResolvedScopeID          string    `json:"resolved_scope_id,omitempty"`
	ResolvedAcceptanceUnitID string    `json:"resolved_acceptance_unit_id,omitempty"`
	ResolvedSourceRunID      string    `json:"resolved_source_run_id,omitempty"`
	ResolvedGenerationID     string    `json:"resolved_generation_id,omitempty"`
}

func wireMutation(mutation ClaimMutation) claimPlaneMutation {
	return claimPlaneMutation{
		WorkItemID: mutation.WorkItemID, ClaimID: mutation.ClaimID,
		FencingToken: mutation.FencingToken, OwnerID: mutation.OwnerID,
		ObservedAt: mutation.ObservedAt, LeaseDuration: int64(mutation.LeaseDuration),
		TenantID: mutation.TenantID, WorkspaceID: mutation.WorkspaceID,
		SubjectClass: mutation.SubjectClass, PolicyRevisionHash: mutation.PolicyRevisionHash,
		FailureClass: mutation.FailureClass, FailureMessage: mutation.FailureMessage,
		FailureOperation: mutation.FailureOperation, FailureStatusCode: mutation.FailureStatusCode,
		FailureTerminal: mutation.FailureTerminal, VisibleAt: mutation.VisibleAt,
		ResolvedScopeID:          mutation.ResolvedScopeID,
		ResolvedAcceptanceUnitID: mutation.ResolvedAcceptanceUnitID,
		ResolvedSourceRunID:      mutation.ResolvedSourceRunID,
		ResolvedGenerationID:     mutation.ResolvedGenerationID,
	}
}

// ClaimNextEligible claims the next work item through the server. An idle
// queue reports (zero, zero, false, nil); a paced queue reports
// *ClaimPacedError carrying the backoff.
func (c *ClaimPlaneClient) ClaimNextEligible(
	ctx context.Context,
	selector ClaimSelector,
	now time.Time,
	leaseDuration time.Duration,
) (WorkItem, Claim, bool, error) {
	var response struct {
		Issued bool `json:"issued"`
		Grant  struct {
			WorkItem claimPlaneWireItem  `json:"work_item"`
			Claim    claimPlaneWireClaim `json:"claim"`
		} `json:"grant,omitempty"`
		Deferral struct {
			Signal     string `json:"signal"`
			Domain     string `json:"domain,omitempty"`
			RetryAfter int64  `json:"retry_after_ns"`
			Stuck      bool   `json:"stuck,omitempty"`
		} `json:"deferral,omitempty"`
	}
	err := c.post(ctx, "ClaimNext", "/workflow.v1/ClaimNext", map[string]any{
		"selector": map[string]any{
			"collector_kind":        string(selector.CollectorKind),
			"collector_instance_id": selector.CollectorInstanceID,
			"owner_id":              selector.OwnerID,
			"claim_id":              selector.ClaimID,
			"rotation_salt":         selector.RotationSalt,
		},
		"observed_at":  now.UTC(),
		"lease_ttl_ns": int64(leaseDuration),
	}, &response)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return WorkItem{}, Claim{}, false, nil
		}
		return WorkItem{}, Claim{}, false, err
	}
	if response.Issued {
		return wireItemToWorkItem(response.Grant.WorkItem),
			wireClaimToClaim(response.Grant.Claim), true, nil
	}
	return WorkItem{}, Claim{}, false, &ClaimPacedError{
		Signal:     ClaimPacingSignal(response.Deferral.Signal),
		Domain:     response.Deferral.Domain,
		RetryAfter: time.Duration(response.Deferral.RetryAfter),
		Stuck:      response.Deferral.Stuck,
	}
}

// mutate posts one fenced mutation to its operation path.
func (c *ClaimPlaneClient) mutate(
	ctx context.Context,
	operation, path string,
	mutation ClaimMutation,
) error {
	var ack struct {
		OK bool `json:"ok"`
	}
	return c.post(ctx, operation, path, wireMutation(mutation), &ack)
}

// HeartbeatClaim extends the live epoch through the server.
func (c *ClaimPlaneClient) HeartbeatClaim(ctx context.Context, mutation ClaimMutation) error {
	return c.mutate(ctx, "HeartbeatClaim", "/workflow.v1/HeartbeatClaim", mutation)
}

// CompleteClaim completes the live epoch through the server.
func (c *ClaimPlaneClient) CompleteClaim(ctx context.Context, mutation ClaimMutation) error {
	return c.mutate(ctx, "CompleteClaim", "/workflow.v1/CompleteClaim", mutation)
}

// ReleaseClaim releases the live epoch through the server.
func (c *ClaimPlaneClient) ReleaseClaim(ctx context.Context, mutation ClaimMutation) error {
	return c.mutate(ctx, "ReleaseClaim", "/workflow.v1/ReleaseClaim", mutation)
}

// FailClaimRetryable fails the live epoch retryable through the server.
func (c *ClaimPlaneClient) FailClaimRetryable(ctx context.Context, mutation ClaimMutation) error {
	return c.mutate(ctx, "FailClaimRetryable", "/workflow.v1/FailClaimRetryable", mutation)
}

// FailClaimTerminal fails the live epoch terminal through the server.
func (c *ClaimPlaneClient) FailClaimTerminal(ctx context.Context, mutation ClaimMutation) error {
	return c.mutate(ctx, "FailClaimTerminal", "/workflow.v1/FailClaimTerminal", mutation)
}

// CheckClaimPacing probes the server pacing verdict for one family. The
// passed config is accepted for ClaimPacingChecker compatibility; the
// server enforces its own configured watermarks.
func (c *ClaimPlaneClient) CheckClaimPacing(
	ctx context.Context,
	selector ClaimSelector,
	_ ClaimPacingConfig,
	now time.Time,
) (ClaimPacingDecision, error) {
	var decision struct {
		Paced      bool   `json:"paced"`
		Signal     string `json:"signal,omitempty"`
		Domain     string `json:"domain,omitempty"`
		RetryAfter int64  `json:"retry_after_ns,omitempty"`
		Stuck      bool   `json:"stuck,omitempty"`
	}
	err := c.post(ctx, "CheckClaimPacing", "/workflow.v1/CheckClaimPacing", map[string]any{
		"collector_kind":        string(selector.CollectorKind),
		"collector_instance_id": selector.CollectorInstanceID,
		"observed_at":           now.UTC(),
	}, &decision)
	if err != nil {
		return ClaimPacingDecision{}, err
	}
	return ClaimPacingDecision{
		Paced:      decision.Paced,
		Signal:     ClaimPacingSignal(decision.Signal),
		Domain:     decision.Domain,
		RetryAfter: time.Duration(decision.RetryAfter),
		Stuck:      decision.Stuck,
	}, nil
}

// PinClaimForCommit pins a live claim for a fact commit and returns the
// commit id core stamps on the generation.
func (c *ClaimPlaneClient) PinClaimForCommit(
	ctx context.Context,
	claimID string,
	fencingToken int64,
	scopeID, sourceRunID, generationID string,
	commitDeadline time.Time,
) (string, error) {
	var response struct {
		Pinned   bool   `json:"pinned"`
		CommitID string `json:"commit_id"`
	}
	err := c.post(ctx, "ValidateAndPinClaimForCommit",
		"/workflow.v1/ValidateAndPinClaimForCommit", map[string]any{
			"claim_id":        claimID,
			"fencing_token":   fencingToken,
			"scope_id":        scopeID,
			"source_run_id":   sourceRunID,
			"generation_id":   generationID,
			"commit_deadline": commitDeadline.UTC(),
		}, &response)
	if err != nil {
		return "", err
	}
	if !response.Pinned {
		return "", fmt.Errorf("claim-plane PinClaimForCommit: server returned unpinned without error")
	}
	return response.CommitID, nil
}

// claimPlaneWireInstance mirrors one protocol collector instance.
type claimPlaneWireInstance struct {
	InstanceID     string    `json:"instance_id"`
	CollectorKind  string    `json:"collector_kind"`
	Mode           string    `json:"mode"`
	Enabled        bool      `json:"enabled"`
	Bootstrap      bool      `json:"bootstrap"`
	ClaimsEnabled  bool      `json:"claims_enabled"`
	DisplayName    string    `json:"display_name,omitempty"`
	Configuration  string    `json:"configuration,omitempty"`
	LastObservedAt time.Time `json:"last_observed_at,omitempty"`
	DeactivatedAt  time.Time `json:"deactivated_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ListCollectorInstances reads durable instances through the server.
func (c *ClaimPlaneClient) ListCollectorInstances(ctx context.Context) ([]CollectorInstance, error) {
	var response struct {
		Instances []claimPlaneWireInstance `json:"instances,omitempty"`
	}
	if err := c.post(ctx, "ListCollectorInstances",
		"/workflow.v1/ListCollectorInstances", nil, &response); err != nil {
		return nil, err
	}
	instances := make([]CollectorInstance, 0, len(response.Instances))
	for _, wire := range response.Instances {
		instances = append(instances, CollectorInstance{
			InstanceID:    wire.InstanceID,
			CollectorKind: scope.CollectorKind(wire.CollectorKind),
			Mode:          CollectorMode(wire.Mode), Enabled: wire.Enabled,
			Bootstrap: wire.Bootstrap, ClaimsEnabled: wire.ClaimsEnabled,
			DisplayName: wire.DisplayName, Configuration: wire.Configuration,
			LastObservedAt: wire.LastObservedAt, DeactivatedAt: wire.DeactivatedAt,
			CreatedAt: wire.CreatedAt, UpdatedAt: wire.UpdatedAt,
		})
	}
	return instances, nil
}

func claimPlaneUnsupported(operation string) error {
	return fmt.Errorf("%w: %s", ErrClaimPlaneUnsupported, operation)
}

// CreateRun stays server-internal: no protocol route admits runs.
func (c *ClaimPlaneClient) CreateRun(context.Context, Run) error {
	return claimPlaneUnsupported("CreateRun")
}

// EnqueueWorkItems stays server-internal: no protocol route admits work.
func (c *ClaimPlaneClient) EnqueueWorkItems(context.Context, []WorkItem) error {
	return claimPlaneUnsupported("EnqueueWorkItems")
}

// ReconcileCollectorInstances stays server-internal.
func (c *ClaimPlaneClient) ReconcileCollectorInstances(
	context.Context,
	time.Time,
	[]DesiredCollectorInstance,
) error {
	return claimPlaneUnsupported("ReconcileCollectorInstances")
}

// UpsertCompletenessStates stays server-internal.
func (c *ClaimPlaneClient) UpsertCompletenessStates(context.Context, []CompletenessState) error {
	return claimPlaneUnsupported("UpsertCompletenessStates")
}

// ReconcileWorkflowRuns stays server-internal (the server reaper owns it).
func (c *ClaimPlaneClient) ReconcileWorkflowRuns(context.Context, time.Time) (int, error) {
	return 0, claimPlaneUnsupported("ReconcileWorkflowRuns")
}

// ReapExpiredClaims stays server-internal (the server reaper owns it).
func (c *ClaimPlaneClient) ReapExpiredClaims(
	context.Context,
	time.Time,
	int,
	time.Duration,
) ([]Claim, error) {
	return nil, claimPlaneUnsupported("ReapExpiredClaims")
}

// The client satisfies the durable store surface for the claim path.
var _ ControlStore = (*ClaimPlaneClient)(nil)
