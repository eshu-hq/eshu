// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Pinner pins a claim held by the extracted claim plane for one fact commit.
// *workflow.ClaimPlaneClient satisfies it. The plane must not reap the claim
// before commitDeadline; repeating a pin with identical arguments returns the
// same commit id, so a retry after an ambiguous timeout is safe.
type Pinner interface {
	PinClaimForCommit(
		ctx context.Context,
		claimID string,
		fencingToken int64,
		scopeID, sourceRunID, generationID string,
		commitDeadline time.Time,
	) (string, error)
}

// CodeFunc returns the workflow.v1 envelope code of a Pinner error, or "" for
// a transport or decode failure. Production passes
// workflow.ClaimPlaneErrorCode.
type CodeFunc func(error) string

// Request is the claim identity one plane-path commit pins and fences on.
// ScopeID, SourceRunID, and GenerationID are the item's PLANNED identity (the
// pin matches the plane's item row on them); the committed generation, which
// terraform-state resolves later, is carried separately on the fence Row.
type Request struct {
	WorkItemID   string
	ClaimID      string
	FencingToken int64
	ScopeID      string
	SourceRunID  string
	GenerationID string
	// Lease is the claim lease the collector holds. Every pin asks for a
	// deadline of now plus Lease, and the keeper re-pins every Lease/3.
	Lease time.Duration
}

// RequestFor builds the pin request from a claim mutation and refuses one
// that lacks any field the plane path needs. The in-tree path never calls it.
func RequestFor(mutation workflow.ClaimMutation) (Request, error) {
	request := Request{
		WorkItemID:   strings.TrimSpace(mutation.WorkItemID),
		ClaimID:      strings.TrimSpace(mutation.ClaimID),
		FencingToken: mutation.FencingToken,
		ScopeID:      strings.TrimSpace(mutation.ScopeID),
		SourceRunID:  strings.TrimSpace(mutation.SourceRunID),
		GenerationID: strings.TrimSpace(mutation.GenerationID),
		Lease:        mutation.LeaseDuration,
	}
	switch {
	case request.WorkItemID == "" || request.ClaimID == "":
		return Request{}, errors.New("claim-plane commit fence: work item id and claim id are required")
	case request.FencingToken <= 0:
		return Request{}, errors.New("claim-plane commit fence: fencing token must be positive")
	case request.ScopeID == "" || request.SourceRunID == "" || request.GenerationID == "":
		return Request{}, errors.New("claim-plane commit fence: planned scope, source run, and generation ids are required")
	case request.Lease <= 0:
		return Request{}, errors.New("claim-plane commit fence: lease duration must be positive")
	}
	return request, nil
}

// Pin pins the claim once, retrying a transport failure up to attempts times
// with identical arguments (the plane keeps the first commit id and only
// extends the deadline, so a retry is idempotent). A stale or unknown claim is
// a terminal Rejection at once; exhausted retries are a retryable
// pin_unavailable Rejection. Pin never falls back to an in-tree heartbeat.
func Pin(
	ctx context.Context,
	pinner Pinner,
	code CodeFunc,
	request Request,
	deadline time.Time,
	attempts int,
	backoff time.Duration,
) (string, error) {
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		commitID, err := pinner.PinClaimForCommit(ctx, request.ClaimID, request.FencingToken,
			request.ScopeID, request.SourceRunID, request.GenerationID, deadline)
		if err == nil {
			return commitID, nil
		}
		if reason, refused := refusalReason(code(err)); refused {
			return "", &Rejection{Reason: reason, RequestedToken: request.FencingToken, Err: err}
		}
		last = err
		if ctx.Err() != nil {
			break
		}
		if attempt < attempts && backoff > 0 {
			timer := time.NewTimer(backoff * time.Duration(attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
	return "", &Rejection{Reason: ReasonPinUnavailable, RequestedToken: request.FencingToken, Err: last}
}

// refusalReason maps a protocol code to a terminal refusal. Codes the plane
// uses for a fenced-away epoch collapse to stale_fence, matching
// workflow.ErrClaimRejected.
func refusalReason(code string) (Reason, bool) {
	switch code {
	case "stale_fence", "lease_expired", "claim_conflict":
		return ReasonStaleFence, true
	case "not_found":
		return ReasonNotFound, true
	default:
		return "", false
	}
}

// CheckDeadline is the last step before COMMIT: the latest pin deadline must
// exceed now by at least margin. It returns the slack (latest minus now) for
// the slack histogram and a retryable deadline Rejection when the slack is
// below margin. now must be the Go clock the deadline was built from, never
// SQL now(), which is the transaction start time.
func CheckDeadline(latest, now time.Time, margin time.Duration) (time.Duration, error) {
	slack := latest.Sub(now)
	if slack < margin {
		return slack, &Rejection{
			Reason: ReasonDeadline,
			Err:    fmt.Errorf("commit pin slack %s is below the %s margin", slack, margin),
		}
	}
	return slack, nil
}
