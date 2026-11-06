// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MaxCommitClaimFactsBytes bounds one CommitClaimFacts result document. It
// pairs with the satellite host's 4 MiB adapter stdout limit: the commit
// carrier never accepts more than the producer can emit, so an unbounded
// satellite cannot exhaust the commit boundary.
//
// Two bounds govern this route, and both must hold. This constant bounds the
// result_json document only. MaxCommitClaimFactsBodyBytes bounds the whole
// request body (result plus mutation envelope, idempotency key, and planned
// IDs). The plane server enforces the same body bound on its read without
// trusting the proxy; the client fails fast before any byte reaches the
// wire. A server enforcing only a smaller total-body cap breaks every
// near-cap result — the exact class this lane exists to carry.
const MaxCommitClaimFactsBytes = 4 << 20

// MaxCommitClaimFactsBodyBytes caps the whole CommitClaimFacts request body
// on this route only: the 4 MiB result plus a 64 KiB envelope allowance for
// the mutation, the idempotency key, the planned IDs, and JSON framing
// (4,194,304 + 65,536 = 4,259,840 B). The 64 KiB covers the bounded
// mutation fields; the body cap itself is the bound on unbounded identifier
// strings, which fail the request closed instead of reaching the server.
// The other claim-plane routes keep their existing 1 MiB body cap.
const MaxCommitClaimFactsBodyBytes = MaxCommitClaimFactsBytes + (64 << 10)

// commitClaimFactsPath is the commit-lane route the plane must serve. It is
// declared here as the cross-repo contract: the satellite adopts this exact
// operation and path when it lands the server half (eshu-hq/eshu#8877), and
// the protocol-version pin test exercises it like every other route.
//
// Server-half admission precondition (tracked on eshu-hq/eshu#8877): the
// satellite serves this route as a dumb byte-verbatim proxy — no decode, no
// claim-store call, no lock held — and forwards the bounded body to core.
// Core serves it synchronously through collector.CommitClaimFacts only after
// its commit handler runs the admission stack (validator.ValidateResult
// against the claim contract, bounded statuses, the full returned-claim
// match, payload-schema validation, and producer-grant coverage), builds the
// scope per collector kind, and matches the planned IDs below against the
// plane's item row. Core never scrapes its planned target out of the result
// echo: the planned IDs travel beside the result, and the echo is checked
// against them — two independent checks, not one self-consistency check.
const commitClaimFactsPath = "/workflow.v1/CommitClaimFacts"

// commitClaimFactsBody is the commit-lane wire shape: the fenced mutation
// identifying the live epoch, the planned scope, source-run, and generation
// IDs core matches against the plane's item row, an opaque idempotency key
// scoping retries to that epoch, and the verbatim bounded collector result.
// The result travels as raw JSON so the client never re-encodes producer
// bytes. The idempotency key binds no commit identity by itself; the
// generation is the only retry key, and the key is carried for wire
// symmetry with the other claim-plane mutations.
type commitClaimFactsBody struct {
	claimPlaneMutation
	ScopeID        string          `json:"scope_id"`
	SourceRunID    string          `json:"source_run_id"`
	GenerationID   string          `json:"generation_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	ResultJSON     json.RawMessage `json:"result_json"`
}

// CommitClaimFacts commits one bounded collector result through the fenced
// epoch and returns only when the bytes are durable. The caller then
// completes the claim with CompleteClaim, which stays state-only: commit
// first, complete second, one fencing token for both. A stale fence, an
// expired lease, an oversized or keyless carrier, or a carrier missing its
// planned IDs fails before any byte is committed; the server answers the
// same {"ok":true} acknowledgement the lifecycle mutations use.
func (c *ClaimPlaneClient) CommitClaimFacts(
	ctx context.Context,
	mutation ClaimMutation,
	idempotencyKey string,
	resultJSON json.RawMessage,
) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return errors.New("claim-plane CommitClaimFacts: idempotency key is required")
	}
	if len(resultJSON) == 0 {
		return errors.New("claim-plane CommitClaimFacts: result is required")
	}
	if len(resultJSON) > MaxCommitClaimFactsBytes {
		return fmt.Errorf(
			"claim-plane CommitClaimFacts: result is %d bytes, over the %d-byte cap",
			len(resultJSON),
			MaxCommitClaimFactsBytes,
		)
	}
	if strings.TrimSpace(mutation.ScopeID) == "" ||
		strings.TrimSpace(mutation.SourceRunID) == "" ||
		strings.TrimSpace(mutation.GenerationID) == "" {
		return errors.New("claim-plane CommitClaimFacts: planned scope, source run, and generation ids are required")
	}
	if err := validateMutationFailureOperation(mutation); err != nil {
		return fmt.Errorf("claim-plane CommitClaimFacts: %w", err)
	}
	body := commitClaimFactsBody{
		claimPlaneMutation: wireMutation(mutation),
		ScopeID:            mutation.ScopeID,
		SourceRunID:        mutation.SourceRunID,
		GenerationID:       mutation.GenerationID,
		IdempotencyKey:     idempotencyKey,
		ResultJSON:         resultJSON,
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("claim-plane CommitClaimFacts: marshal: %w", err)
	}
	if len(rawBody) > MaxCommitClaimFactsBodyBytes {
		return fmt.Errorf(
			"claim-plane CommitClaimFacts: body is %d bytes, over the %d-byte cap",
			len(rawBody),
			MaxCommitClaimFactsBodyBytes,
		)
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	// Pass the already-marshaled body through: post would re-marshal the
	// struct byte-identically (json.RawMessage embeds verbatim), and the
	// length above is the exact wire length this way.
	if err := c.post(ctx, "CommitClaimFacts", commitClaimFactsPath, json.RawMessage(rawBody), &ack); err != nil {
		return err
	}
	if !ack.OK {
		return errors.New("claim-plane CommitClaimFacts: server returned ok=false")
	}
	return nil
}
