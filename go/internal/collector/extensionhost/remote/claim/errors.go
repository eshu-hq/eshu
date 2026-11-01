// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"
)

// CallError is one failed protocol call. Code is the stable contract
// (a workflowv1 code, or CodeResponseTooLarge for a client-side bound);
// Message is bounded operator detail that never carries credentials.
type CallError struct {
	Op         workflowv1.Operation
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *CallError) Error() string {
	return fmt.Sprintf("%s %s: %s", e.Op, e.Code, e.Message)
}

// Stale reports a 409-class conflict: the held epoch is no longer honored.
// The caller must re-claim, never retry the same mutation.
func (e *CallError) Stale() bool {
	return e.Code == workflowv1.CodeStaleFence ||
		e.Code == workflowv1.CodeLeaseExpired ||
		e.Code == workflowv1.CodeClaimConflict
}

// Paced reports flow control: no claim was issued; back off for RetryAfter.
func (e *CallError) Paced() bool { return e.Code == workflowv1.CodePaced }

// NotFound reports a missing row.
func (e *CallError) NotFound() bool { return e.Code == workflowv1.CodeNotFound }

// VersionMismatch reports a protocol-major disagreement.
func (e *CallError) VersionMismatch() bool { return e.Code == workflowv1.CodeVersionMismatch }

// PacedError is a paced ClaimNext: no claim was issued. It mirrors the
// in-tree pacing-deferral-as-error shape so the runner backs off on the
// existing retry-after path. RetryAfter is always the grant delay to honor.
type PacedError struct {
	// Deferral is the server's pacing verdict on the issued=false path.
	// It is zero on the 429 path, where the envelope carries only delay.
	// The domain is scrubbed of the bearer and bounded before it is stored.
	Deferral   workflowv1.ClaimPacingDeferral
	RetryAfter time.Duration
	// detail is the pre-rendered, scrubbed, bounded operator text for the
	// issued=false path: the server-supplied domain never reaches Error
	// unbounded.
	detail string
}

func (e *PacedError) Error() string {
	if e.detail != "" {
		return e.detail
	}
	return fmt.Sprintf("claim pacing engaged: grant delayed %s", e.RetryAfter)
}

// RetryAfterDelay exposes the grant delay on the retry-after path.
func (e *PacedError) RetryAfterDelay() time.Duration { return e.RetryAfter }

// deferralError builds the issued=false paced error. The server-supplied
// domain is scrubbed and bounded in the stored deferral, and the rendered
// text is scrubbed then bounded once more.
func (c *Client) deferralError(deferral workflowv1.ClaimPacingDeferral) error {
	deferral.Domain = boundMessage(c.scrub(deferral.Domain))
	return &PacedError{
		Deferral:   deferral,
		RetryAfter: deferral.RetryAfter,
		detail:     boundMessage(c.scrub(deferral.Error())),
	}
}

// pacedError builds the 429-path paced error from the envelope delay.
func (c *Client) pacedError(envelope workflowv1.ErrorEnvelope) error {
	var retryAfter time.Duration
	if envelope.RetryAfterSeconds > 0 {
		retryAfter = time.Duration(envelope.RetryAfterSeconds) * time.Second
	}
	return &PacedError{RetryAfter: retryAfter}
}

// callError renders a protocol failure. The message is the server's bounded
// detail, scrubbed of credentials; an unparseable body degrades to the
// status text rather than failing to produce an error.
func (c *Client) callError(
	op workflowv1.Operation,
	status int,
	envelope workflowv1.ErrorEnvelope,
) error {
	code := boundMessage(c.scrub(envelope.Code))
	message := envelope.Message
	if strings.TrimSpace(code) == "" {
		code = "unexpected_status"
		message = http.StatusText(status)
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("unexpected status %d", status)
		}
	}
	callErr := &CallError{Op: op, Code: code, Message: boundMessage(c.scrub(message))}
	if envelope.RetryAfterSeconds > 0 {
		callErr.RetryAfter = time.Duration(envelope.RetryAfterSeconds) * time.Second
	}
	return callErr
}

// callErrorText renders a client-side failure (decode, bound, transport).
func (c *Client) callErrorText(op workflowv1.Operation, code, message string) error {
	return &CallError{Op: op, Code: code, Message: boundMessage(c.scrub(message))}
}

// scrub removes the configured bearer from surfaced text: a hostile server
// that echoes the Authorization value in an error message must not have it
// relayed to operators.
func (c *Client) scrub(message string) string {
	if c.token == "" {
		return message
	}
	return strings.ReplaceAll(message, c.token, "[redacted]")
}

// boundMessage truncates surfaced detail to the operator bound on a rune
// boundary. Callers scrub credentials first: truncating before scrubbing
// would cut a token that straddles the bound and leave a prefix the whole-
// string scrub no longer matches.
func boundMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if len(trimmed) <= maxErrorMessage {
		return trimmed
	}
	cut := maxErrorMessage
	for cut > 0 && !utf8.RuneStart(trimmed[cut]) {
		cut--
	}
	return trimmed[:cut] + "…"
}
