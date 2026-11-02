// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Span name of the claim-plane commit fence (#9376). It lives in this sibling
// file so the frozen root contract.go does not grow.
const (
	// SpanIngestionCommitClaimFence wraps one plane-path ingestion commit
	// from the pre-transaction pin to COMMIT or ROLLBACK. It carries
	// fence_mode, claim_id, fencing_token, work_item_id, generation_id,
	// commit_id, and outcome as span attributes; metrics never carry ids.
	SpanIngestionCommitClaimFence = "ingestion.commit.claim_fence"
)
