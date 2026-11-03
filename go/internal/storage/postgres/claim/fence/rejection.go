// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import "fmt"

// Reason is the bounded label for why the plane-path commit fence refused a
// commit. It is the reason label of
// eshu_dp_ingestion_claim_fence_rejections_total.
type Reason string

const (
	// ReasonStaleFence means the plane no longer holds the claim live at
	// this token (reaped, reclaimed, or completed). Terminal.
	ReasonStaleFence Reason = "stale_fence"
	// ReasonNotFound means the plane does not know the claim. Terminal.
	ReasonNotFound Reason = "not_found"
	// ReasonPinUnavailable means the pin did not answer after the identical
	// retries. Retryable; no transaction was opened.
	ReasonPinUnavailable Reason = "pin_unavailable"
	// ReasonDeadline means the latest pin deadline was within the margin of
	// the Go clock at COMMIT time. Retryable; the transaction rolled back.
	ReasonDeadline Reason = "deadline"
	// ReasonFenceToken means a higher token already committed this
	// (generation, work item). Terminal: this writer is stale.
	ReasonFenceToken Reason = "fence_token"
	// ReasonFenceClaimMismatch means the same token was committed under a
	// different claim id. The plane's UNIQUE (work_item_id, fencing_token)
	// forbids that, so it is an integrity alarm. Terminal.
	ReasonFenceClaimMismatch Reason = "fence_claim_mismatch"
)

// Reasons returns the closed label set in a fixed order.
func Reasons() []Reason {
	return []Reason{
		ReasonStaleFence, ReasonNotFound, ReasonPinUnavailable,
		ReasonDeadline, ReasonFenceToken, ReasonFenceClaimMismatch,
	}
}

// Rejection is the error a plane-path commit returns when the fence refuses
// it. It implements the collector's terminalFailure contract
// (TerminalFailure) so a stale writer fails its claim terminally instead of
// retrying, while deadline and pin_unavailable stay retryable.
type Rejection struct {
	Reason         Reason
	RequestedToken int64
	StoredToken    int64
	Err            error
}

// Error renders the reason, the tokens when known, and the cause.
func (r *Rejection) Error() string {
	message := fmt.Sprintf("claim-plane commit fence refused: %s", r.Reason)
	if r.StoredToken > 0 {
		message += fmt.Sprintf(" (requested token %d, stored token %d)", r.RequestedToken, r.StoredToken)
	}
	if r.Err != nil {
		message += ": " + r.Err.Error()
	}
	return message
}

// Unwrap exposes the underlying cause.
func (r *Rejection) Unwrap() error { return r.Err }

// TerminalFailure reports whether the refusal ends the claim: true for a
// stale, unknown, or out-fenced writer, false for deadline and
// pin_unavailable, where a retry under a live claim can still succeed.
func (r *Rejection) TerminalFailure() bool {
	switch r.Reason {
	case ReasonDeadline, ReasonPinUnavailable:
		return false
	default:
		return true
	}
}

// FailureOperation is the bounded claim-stage token the collector records on
// the failed claim.
func (r *Rejection) FailureOperation() string { return "commit_claim_fence" }
