// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"testing"
)

const (
	failureReplayBody     = `{"scope_id":"scope-a","reason":"bounded replay","idempotency_key":"key-1"}`
	failureReplayByIDBody = `{"work_item_ids":["wi-1"],"reason":"bounded replay","idempotency_key":"key-1"}`
	failureReopenBody     = `{"domain":"repo_dependency","scope_id":"scope-a","reason":"bounded reopen","idempotency_key":"key-1"}`
	failureRecoverBody    = `{"scope_ids":["scope-a"],"reason":"bounded recovery","idempotency_key":"key-1"}`
)

// claimedFailureStep is an idempotency claim this request won.
var claimedFailureStep = ReplayIdempotencyClaim{Claimed: true}

// TestAdminInspectionFailuresAnswerFixedText drives every admin inspection
// read through a backend fault, a client cancel, and a stale reader, and the
// three bounded reads through their route timeout too (#7674).
func TestAdminInspectionFailuresAnswerFixedText(t *testing.T) {
	t.Parallel()
	runAdminFailureSteps(t, []adminFailureStep{
		{
			name: "work items", path: "/api/v0/admin/work-items/query", body: `{}`,
			message: workItemQueryFailedMessage, reader: true,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{workItemsErr: err}} },
		},
		{
			name: "decisions", path: "/api/v0/admin/decisions/query",
			body:    `{"repository_id":"repo-a","source_run_id":"run-a"}`,
			message: decisionQueryFailedMessage, reader: true,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{decisionsErr: err}} },
		},
		{
			name: "decision evidence", path: "/api/v0/admin/decisions/query",
			body:    `{"repository_id":"repo-a","source_run_id":"run-a","include_evidence":true}`,
			message: evidenceQueryFailedMessage, reader: true,
			handler: func(err error) *Handler {
				return &Handler{Store: &stubAdminStore{decisions: []DecisionRow{{DecisionID: "d-1"}}, evidenceErr: err}}
			},
		},
		{
			name: "replay events", path: "/api/v0/admin/replay-events/query", body: `{}`,
			message: replayEventQueryFailedMessage, reader: true,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{replayEvtErr: err}} },
		},
		{
			name: "dead letters", path: "/api/v0/admin/dead-letters/query", body: `{"limit":10,"timeout_ms":5000}`,
			message: deadLetterListFailedMessage, reader: true, timeoutMessage: deadLetterListTimeoutMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{deadLetterErr: err}} },
		},
		{
			name: "input invalid facts", path: "/api/v0/admin/input-invalid-facts/query",
			body:    `{"scope_id":"scope-a","generation_id":"gen-a","limit":10,"timeout_ms":5000}`,
			message: inputInvalidFactListFailedMessage, reader: true, timeoutMessage: inputInvalidFactListTimeoutMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{inputInvalidFactErr: err}} },
		},
		{
			name: "changed-since poisoned links", path: "/api/v0/admin/changed-since/poisoned-links/query",
			body:    `{"limit":10,"timeout_ms":5000}`,
			message: poisonedLinkListFailedMessage, reader: true, timeoutMessage: poisonedLinkListTimeoutMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{changedSincePoisonedLinkErr: err}} },
		},
	})
}

// TestAdminMutationFailuresAnswerFixedText drives every admin mutation step
// through a backend fault and a client cancel (#7674). The repository
// reindex selector lookup reads the content store through the guarded
// reader, so it also meets a stale reader.
func TestAdminMutationFailuresAnswerFixedText(t *testing.T) {
	t.Parallel()
	runAdminFailureSteps(t, []adminFailureStep{
		{
			name: "refinalize", path: "/api/v0/admin/refinalize", body: `{"scope_ids":["scope-a"]}`,
			message: refinalizeFailedMessage,
			handler: func(err error) *Handler { return &Handler{Recovery: &stubRecoveryHandler{refinalizeErr: err}} },
		},
		{
			name: "workspace reindex", path: "/api/v0/admin/reindex", body: `{"ingester":"repository"}`,
			message: reindexFailedMessage,
			handler: func(err error) *Handler { return &Handler{Reindexer: &stubReindexRequester{err: err}} },
		},
		{
			name: "repository reindex lookup", path: "/api/v0/admin/reindex",
			body:    `{"ingester":"repository","scope":"repository","repositories":["payments"]}`,
			message: repositoryReindexLookupFailedMessage, reader: true,
			handler: func(err error) *Handler {
				return &Handler{Repositories: stubRepositoryCatalog{err: err}, RepositoryReindexer: &stubRepositoryReindexer{}}
			},
		},
		{
			name: "repository reindex request", path: "/api/v0/admin/reindex",
			body:    `{"ingester":"repository","scope":"repository","repositories":["payments"]}`,
			message: repositoryReindexRequestFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Repositories: repositoryCatalogFixture(), RepositoryReindexer: &stubRepositoryReindexer{err: err}}
			},
		},
		{
			name: "recover generations claim", path: "/api/v0/admin/recover-generations", body: failureRecoverBody,
			message: recoverGenerationsClaimFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Recovery: &stubRecoveryHandler{}, Store: &stubAdminStore{claimErr: err}}
			},
		},
		{
			name: "recover generations", path: "/api/v0/admin/recover-generations", body: failureRecoverBody,
			message: recoverGenerationsFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Recovery: &stubRecoveryHandler{refinalizeErr: err}, Store: &stubAdminStore{claim: claimedFailureStep}}
			},
		},
		{
			name: "recover generations record", path: "/api/v0/admin/recover-generations", body: failureRecoverBody,
			message: recoverGenerationsRecordFailedMessage, finalize: true,
			handler: func(err error) *Handler {
				return &Handler{Recovery: &stubRecoveryHandler{}, Store: &stubAdminStore{claim: claimedFailureStep, completeErr: err}}
			},
		},
		{
			name: "dead-letter", path: "/api/v0/admin/dead-letter", body: `{"scope_id":"scope-a"}`,
			message: deadLetterMutationFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{deadLetterErr: err}} },
		},
		{
			name: "skip", path: "/api/v0/admin/skip", body: `{"repository_id":"repo-a"}`,
			message: skipFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{skipErr: err}} },
		},
		{
			name: "backfill", path: "/api/v0/admin/backfill", body: `{"scope_id":"scope-a"}`,
			message: backfillFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{backfillErr: err}} },
		},
	})
}

// TestAdminIdempotentMutationFailuresAnswerFixedText drives each step of the
// ledger-backed replay and reopen routes through a backend fault and a
// client cancel (#7674).
func TestAdminIdempotentMutationFailuresAnswerFixedText(t *testing.T) {
	t.Parallel()
	runAdminFailureSteps(t, []adminFailureStep{
		{
			name: "replay superseded check", path: "/api/v0/admin/replay", body: failureReplayByIDBody,
			message: replaySupersededCheckFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{supersededErr: err}} },
		},
		{
			name: "replay unsafe check", path: "/api/v0/admin/replay", body: failureReplayByIDBody,
			message: replayUnsafeCheckFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{unsafeTargetsErr: err}} },
		},
		{
			name: "replay claim", path: "/api/v0/admin/replay", body: failureReplayBody,
			message: replayClaimFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{claimErr: err}} },
		},
		{
			name: "replay", path: "/api/v0/admin/replay", body: failureReplayBody,
			message: replayFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Store: &stubAdminStore{claim: claimedFailureStep, replayErr: err}}
			},
		},
		{
			name: "replay record", path: "/api/v0/admin/replay", body: failureReplayBody,
			message: replayRecordFailedMessage, finalize: true,
			handler: func(err error) *Handler {
				return &Handler{Store: &stubAdminStore{claim: claimedFailureStep, completeErr: err}}
			},
		},
		{
			name: "reopen resolve", path: "/api/v0/admin/reopen", body: failureReopenBody,
			message: reopenResolveFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{resolveErr: err}} },
		},
		{
			name: "reopen claim", path: "/api/v0/admin/reopen", body: failureReopenBody,
			message: reopenClaimFailedMessage,
			handler: func(err error) *Handler { return &Handler{Store: &stubAdminStore{claimErr: err}} },
		},
		{
			name: "reopen", path: "/api/v0/admin/reopen", body: failureReopenBody,
			message: reopenFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Store: &stubAdminStore{claim: claimedFailureStep, reopenErr: err}}
			},
		},
		{
			name: "reopen record", path: "/api/v0/admin/reopen", body: failureReopenBody,
			message: reopenRecordFailedMessage, finalize: true,
			handler: func(err error) *Handler {
				return &Handler{Store: &stubAdminStore{claim: claimedFailureStep, completeErr: err}}
			},
		},
	})
}
