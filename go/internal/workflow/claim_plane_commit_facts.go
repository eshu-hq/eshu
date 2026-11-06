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
// satellite cannot exhaust the commit boundary. The plane server enforces
// the same bound; the client fails fast before any byte reaches the wire.
const MaxCommitClaimFactsBytes = 4 << 20

// commitClaimFactsPath is the commit-lane route the plane must serve. It is
// declared here as the cross-repo contract: the satellite adopts this exact
// operation and path when it lands the server half (eshu-hq/eshu#8877), and
// the protocol-version pin test exercises it like every other route.
const commitClaimFactsPath = "/workflow.v1/CommitClaimFacts"

// commitClaimFactsBody is the commit-lane wire shape: the fenced mutation
// identifying the live epoch, an opaque idempotency key scoping retries to
// that epoch, and the verbatim bounded collector result. The result travels
// as raw JSON so the client never re-encodes producer bytes.
type commitClaimFactsBody struct {
	claimPlaneMutation
	IdempotencyKey string          `json:"idempotency_key"`
	ResultJSON     json.RawMessage `json:"result_json"`
}

// CommitClaimFacts commits one bounded collector result through the fenced
// epoch and returns only when the bytes are durable. The caller then
// completes the claim with CompleteClaim, which stays state-only: commit
// first, complete second, one fencing token for both. A stale fence, an
// expired lease, or an oversized or keyless carrier fails before any byte
// is committed; the server answers the same {"ok":true} acknowledgement
// the lifecycle mutations use.
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
	if err := validateMutationFailureOperation(mutation); err != nil {
		return fmt.Errorf("claim-plane CommitClaimFacts: %w", err)
	}
	body := commitClaimFactsBody{
		claimPlaneMutation: wireMutation(mutation),
		IdempotencyKey:     idempotencyKey,
		ResultJSON:         resultJSON,
	}
	var ack struct {
		OK bool `json:"ok"`
	}
	if err := c.post(ctx, "CommitClaimFacts", commitClaimFactsPath, body, &ack); err != nil {
		return err
	}
	if !ack.OK {
		return errors.New("claim-plane CommitClaimFacts: server returned ok=false")
	}
	return nil
}
