// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrCommitClaimFactsTooLarge reports a result document past the byte cap.
// The payload is fixed: it can never shrink on retry, so callers map this
// to a terminal outcome, never a retryable one.
var ErrCommitClaimFactsTooLarge = errors.New("commit claim facts result exceeds the byte cap")

// CommitClaimFactsRequest carries one bounded remote fact commit: the fenced
// epoch the claim plane holds, the planned commit target, an opaque retry
// key scoped to that epoch, and the verbatim bounded collector result.
type CommitClaimFactsRequest struct {
	Mutation       workflow.ClaimMutation
	Scope          scope.IngestionScope
	Generation     scope.ScopeGeneration
	IdempotencyKey string
	ResultJSON     []byte
}

// CommitClaimFacts persists one bounded collector result through the
// collector-owned commit boundary without completing the claim. It is the
// commit half of commit-then-complete in one fenced epoch: the caller runs
// this first, then CompleteClaim (state-only, unchanged) under the same
// fencing token.
//
// The order is fixed: shape and size checks first (no plane contact for a
// carrier that can never commit), then the fence probe, then strict decode
// and identity checks, then the single committer write. The fence probe is
// a HeartbeatClaim, which runs the same fenced-update guard family as the
// Complete path (fencing token, owner, and live lease must match or the
// epoch is rejected): a stale fence fails before any byte is decoded, so
// no partial facts can land. The committer re-validates the fence inside
// its own transaction, so a lease lost between probe and commit still fails
// closed. Retries carry the same idempotency key; the boundary's
// generation-finalized skip makes a replayed commit a nil no-op.
//
// No new metrics: the existing commit and claim paths already instrument
// the boundary this writes through.
func CommitClaimFacts(
	ctx context.Context,
	store ClaimControlStore,
	committer Committer,
	req CommitClaimFactsRequest,
) error {
	if store == nil {
		return errors.New("commit claim facts: claim control store is required")
	}
	if committer == nil {
		return errors.New("commit claim facts: collector committer is required")
	}
	if err := validateClaimPlaneFence(store, committer); err != nil {
		return err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return errors.New("commit claim facts: idempotency key is required")
	}
	if len(req.ResultJSON) > workflow.MaxCommitClaimFactsBytes {
		return fmt.Errorf(
			"%w: result is %d bytes, over the %d-byte cap",
			ErrCommitClaimFactsTooLarge,
			len(req.ResultJSON),
			workflow.MaxCommitClaimFactsBytes,
		)
	}
	// Fence and lease probe before decode: a dead epoch rejects here, and
	// the heartbeat extends the lease to cover the commit that follows.
	if err := store.HeartbeatClaim(ctx, req.Mutation); err != nil {
		return fmt.Errorf("commit claim facts fence probe: %w", err)
	}
	result, err := decodeCommitClaimResult(req.ResultJSON)
	if err != nil {
		return err
	}
	if err := checkCommitClaimIdentity(req, result); err != nil {
		return err
	}
	envelopes, err := mapCommitClaimEnvelopes(req, result)
	if err != nil {
		return err
	}
	claimed, ok := committer.(ClaimedCommitter)
	if !ok {
		return errors.New("claim-aware collector committer must implement ClaimedCommitter")
	}
	if err := claimed.CommitClaimedScopeGeneration(
		ctx,
		req.Mutation,
		req.Scope,
		req.Generation,
		FactsFromSlice(req.Scope, req.Generation, envelopes).Facts,
	); err != nil {
		return fmt.Errorf("commit claim facts commit: %w", err)
	}
	return nil
}

// decodeCommitClaimResult strict-decodes one collector result: unknown
// fields and trailing data fail closed, so a carrier the producer changed
// under us never commits partially-understood bytes.
func decodeCommitClaimResult(raw []byte) (sdkcollector.Result, error) {
	var result sdkcollector.Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return sdkcollector.Result{}, fmt.Errorf("commit claim facts decode result: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return sdkcollector.Result{}, errors.New("commit claim facts decode result: trailing data after JSON document")
	}
	return result, nil
}

// checkCommitClaimIdentity proves the result belongs to the fenced epoch:
// only complete results commit, and the echoed work item, generation, and
// fencing token must match the mutation and target exactly.
func checkCommitClaimIdentity(req CommitClaimFactsRequest, result sdkcollector.Result) error {
	if result.State != sdkcollector.ResultComplete {
		return fmt.Errorf("commit claim facts: result state %q is not complete", result.State)
	}
	if strings.TrimSpace(result.Claim.WorkItemID) != strings.TrimSpace(req.Mutation.WorkItemID) {
		return errors.New("commit claim facts: result work item does not match the fenced epoch")
	}
	if strings.TrimSpace(result.Generation.ID) != strings.TrimSpace(req.Generation.GenerationID) {
		return errors.New("commit claim facts: result generation does not match the fenced target")
	}
	echoed, err := strconv.ParseInt(strings.TrimSpace(result.Claim.FencingToken), 10, 64)
	if err != nil {
		return errors.New("commit claim facts: result fencing token is not a base-10 integer")
	}
	if echoed != req.Mutation.FencingToken {
		return errors.New("commit claim facts: result fencing token does not match the fenced epoch")
	}
	return nil
}

// mapCommitClaimEnvelopes converts SDK facts to commit envelopes. Scope and
// generation come from the fenced target, never the source echo, and every
// fact's echoed refs must name that same target: facts for another scope or
// generation fail closed before the stream opens. FactID derives
// deterministically from the committed content so a replayed commit upserts
// the same rows instead of duplicating them.
func mapCommitClaimEnvelopes(req CommitClaimFactsRequest, result sdkcollector.Result) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(result.Facts))
	for i := range result.Facts {
		fact := result.Facts[i]
		if strings.TrimSpace(fact.Kind) == "" {
			return nil, fmt.Errorf("commit claim facts: fact %d has no kind", i)
		}
		if strings.TrimSpace(fact.StableKey) == "" {
			return nil, fmt.Errorf("commit claim facts: fact %d has no stable key", i)
		}
		if strings.TrimSpace(fact.SourceRef.ScopeID) != strings.TrimSpace(req.Scope.ScopeID) {
			return nil, fmt.Errorf("commit claim facts: fact %d scope does not match the fenced target", i)
		}
		if strings.TrimSpace(fact.SourceRef.GenerationID) != strings.TrimSpace(req.Generation.GenerationID) {
			return nil, fmt.Errorf("commit claim facts: fact %d generation does not match the fenced target", i)
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID: facts.StableID("CommitClaimFact", map[string]any{
				"fact_kind":       fact.Kind,
				"generation_id":   req.Generation.GenerationID,
				"scope_id":        req.Scope.ScopeID,
				"stable_fact_key": fact.StableKey,
			}),
			ScopeID:          req.Scope.ScopeID,
			GenerationID:     req.Generation.GenerationID,
			FactKind:         fact.Kind,
			StableFactKey:    fact.StableKey,
			SchemaVersion:    fact.SchemaVersion,
			CollectorKind:    strings.TrimSpace(result.Claim.CollectorKind),
			FencingToken:     req.Mutation.FencingToken,
			SourceConfidence: string(fact.SourceConfidence),
			ObservedAt:       fact.ObservedAt,
			Payload:          fact.Payload,
			IsTombstone:      fact.Tombstone,
			SourceRef: facts.Ref{
				SourceSystem:   fact.SourceRef.SourceSystem,
				ScopeID:        fact.SourceRef.ScopeID,
				GenerationID:   fact.SourceRef.GenerationID,
				FactKey:        fact.SourceRef.FactKey,
				SourceURI:      fact.SourceRef.URI,
				SourceRecordID: fact.SourceRef.RecordID,
			},
		})
	}
	return envelopes, nil
}
