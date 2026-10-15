// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Fixed client-facing messages for admin route failures (#7674). Each route
// step that answers a server failure has its own constant, and the response
// body is exactly that text: a store or recovery error quotes SQL, relation
// names, hosts, and connection settings, so err.Error() never reaches a 5xx
// body. The cause is recorded on the request span instead.
const (
	refinalizeFailedMessage = "refinalize failed"

	reindexFailedMessage                  = "reindex request failed"
	repositoryReindexLookupFailedMessage  = "repository reindex selector lookup failed"
	repositoryReindexRequestFailedMessage = "repository reindex request failed"

	recoverGenerationsClaimFailedMessage  = "recover-generations idempotency claim failed"
	recoverGenerationsFailedMessage       = "recover-generations failed"
	recoverGenerationsRecordFailedMessage = "recover-generations applied but recording its idempotency outcome failed"

	workItemQueryFailedMessage    = "work-item query failed"
	decisionQueryFailedMessage    = "projection decision query failed"
	evidenceQueryFailedMessage    = "projection decision evidence query failed"
	replayEventQueryFailedMessage = "replay-event query failed"

	deadLetterListFailedMessage        = "dead-letter query failed"
	deadLetterListTimeoutMessage       = "dead-letter query timed out"
	inputInvalidFactListFailedMessage  = "input-invalid-facts query failed"
	inputInvalidFactListTimeoutMessage = "input-invalid-facts query timed out"
	poisonedLinkListFailedMessage      = "changed-since poisoned-links query failed"
	poisonedLinkListTimeoutMessage     = "changed-since poisoned-links query timed out"
	deadLetterMutationFailedMessage    = "dead-letter request failed"
	skipFailedMessage                  = "skip request failed"
	backfillFailedMessage              = "backfill request failed"
	replayUnsafeCheckFailedMessage     = "replay unsafe-target check failed"
	replaySupersededCheckFailedMessage = "replay superseded-generation check failed"
	replayClaimFailedMessage           = "replay idempotency claim failed"
	replayFailedMessage                = "replay failed"
	replayRecordFailedMessage          = "replay applied but recording its idempotency outcome failed"
	reopenResolveFailedMessage         = "reopen scope resolution failed"
	reopenClaimFailedMessage           = "reopen idempotency claim failed"
	reopenFailedMessage                = "reopen failed"
	reopenRecordFailedMessage          = "reopen applied but recording its idempotency outcome failed"
)

// Capabilities of the three bounded operator reads. These are the
// capability-matrix rows the matching MCP tools answer under, so the reader
// verdict envelope names them.
const (
	deadLetterListCapability       = "operator.dead_letters.list"
	inputInvalidFactListCapability = "operator.reducer_input_invalid_facts.list"
	poisonedLinkListCapability     = "operator.changed_since_poisoned_links.list"
)

// writeInspectionFailure answers a failed admin read that has no registered
// capability: work items, decisions and their evidence, replay events, and
// the repository reindex catalog lookup. These reads go through the guarded
// PostgreSQL reader, so a stale or timed-out reader first gets the shared
// retryable 503 with Retry-After (and a graph-read deadline its 504) from
// querycontract.WriteGraphReadError. The routes are operator tooling not in
// the capability matrix, so the verdict names querycontract.NoCapability and
// its envelope carries no capability field (#8176 registers their keys).
// Anything else answers message through tracing.WriteServerFailure: 500 with
// err recorded on the request span, or 499 with only the client-cancel event
// when the caller canceled.
func writeInspectionFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	if querycontract.WriteGraphReadError(w, r, err, querycontract.NoCapability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}

// writeBoundedReadFailure answers a failed bounded operator read that runs
// under its own route timeout (ctx, derived from the request context). A
// stale or timed-out PostgreSQL reader gets the shared retryable 503 with
// Retry-After from querycontract.WriteGraphReadError under capability. A
// deadline on ctx then answers timeoutMessage with 504, and anything else
// answers message with 500, both through tracing.WriteServerFailure (499 on a
// client cancel). The stale-reader verdict wraps context.DeadlineExceeded
// too, so it must run first or a retryable 503 would read as a 504.
func writeBoundedReadFailure(
	w http.ResponseWriter, r *http.Request, ctx context.Context, err error, capability, timeoutMessage, message string,
) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		tracing.WriteServerFailure(w, r, err, http.StatusGatewayTimeout, timeoutMessage)
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}

// writeMutationFailure answers a failed admin mutation step with message
// through tracing.WriteServerFailure: 500 with err on the request span, or
// 499 when the caller canceled the request. Mutations run on the writer, not
// the guarded reader, so no reader verdict applies. A 499 here says only that
// the client left; it does not say the mutation did not apply. Each call site
// states what may already have committed when its step fails. The post-commit
// idempotency finalize does not use it: it fails through writeFinalizeFailure
// (finalize.go), which is always a 500.
func writeMutationFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
