// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"errors"
	"net"
	"time"
)

// ClaimPlaneOutcome is the closed classification of one claim-plane request,
// reported to ClaimPlaneClient.Observe. The set is bounded so it is safe as a
// metric label.
type ClaimPlaneOutcome string

const (
	// ClaimPlaneOutcomeOK is a 200 response that decoded cleanly.
	ClaimPlaneOutcomeOK ClaimPlaneOutcome = "ok"
	// ClaimPlaneOutcomeRejected is a fenced-away epoch (stale_fence,
	// lease_expired, claim_conflict); the caller sees ErrClaimRejected.
	ClaimPlaneOutcomeRejected ClaimPlaneOutcome = "rejected"
	// ClaimPlaneOutcomeNotFound is a not_found envelope. On ClaimNext it is
	// the idle queue, not a failure.
	ClaimPlaneOutcomeNotFound ClaimPlaneOutcome = "not_found"
	// ClaimPlaneOutcomePaced is a paced envelope.
	ClaimPlaneOutcomePaced ClaimPlaneOutcome = "paced"
	// ClaimPlaneOutcomeInvalid is an invalid or version_mismatch envelope:
	// the request or protocol version was refused.
	ClaimPlaneOutcomeInvalid ClaimPlaneOutcome = "invalid"
	// ClaimPlaneOutcomeServerError is any other non-200 response, including
	// a body that is not a protocol envelope (for example a proxy 502).
	ClaimPlaneOutcomeServerError ClaimPlaneOutcome = "server_error"
	// ClaimPlaneOutcomeTimeout is a request that hit the HTTP client timeout
	// or the caller's context deadline.
	ClaimPlaneOutcomeTimeout ClaimPlaneOutcome = "timeout"
	// ClaimPlaneOutcomeCanceled is a request the caller's context canceled
	// before it finished, for example during a SIGTERM shutdown. It is not a
	// claim-plane fault.
	ClaimPlaneOutcomeCanceled ClaimPlaneOutcome = "canceled"
	// ClaimPlaneOutcomeTransportError is any other failure to send the
	// request or read the response (refused connection, reset, DNS).
	ClaimPlaneOutcomeTransportError ClaimPlaneOutcome = "transport_error"
	// ClaimPlaneOutcomeDecodeError is a 200 response whose body did not
	// decode into the operation's response shape.
	ClaimPlaneOutcomeDecodeError ClaimPlaneOutcome = "decode_error"
	// ClaimPlaneOutcomeRequestError is a request the client could not build
	// (marshal or URL failure); nothing reached the network.
	ClaimPlaneOutcomeRequestError ClaimPlaneOutcome = "request_error"
)

// ClaimPlaneObserver receives one call per claim-plane request, after the
// request finishes. operation is the protocol operation name (for example
// "ClaimNext" or "HeartbeatClaim"); elapsed covers the full round trip. It
// must be safe for concurrent use and must not block.
type ClaimPlaneObserver func(ctx context.Context, operation string, outcome ClaimPlaneOutcome, elapsed time.Duration)

// envelopeOutcome maps a protocol envelope code to its outcome.
func envelopeOutcome(code string) ClaimPlaneOutcome {
	switch code {
	case "stale_fence", "lease_expired", "claim_conflict":
		return ClaimPlaneOutcomeRejected
	case "not_found":
		return ClaimPlaneOutcomeNotFound
	case "paced":
		return ClaimPlaneOutcomePaced
	case "invalid", "version_mismatch":
		return ClaimPlaneOutcomeInvalid
	default:
		return ClaimPlaneOutcomeServerError
	}
}

// transportOutcome separates timeouts and caller cancellations from other
// transport failures so an operator can tell a slow claim plane, a rollout,
// and an unreachable claim plane apart.
func transportOutcome(err error) ClaimPlaneOutcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return ClaimPlaneOutcomeTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ClaimPlaneOutcomeCanceled
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ClaimPlaneOutcomeTimeout
	}
	return ClaimPlaneOutcomeTransportError
}
