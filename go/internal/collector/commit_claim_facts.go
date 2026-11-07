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
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrCommitClaimFactsTooLarge reports a result document past the byte cap.
// The payload is fixed: it can never shrink on retry, so callers map this
// to a terminal outcome, never a retryable one.
var ErrCommitClaimFactsTooLarge = errors.New("commit claim facts result exceeds the byte cap")

// commitClaimSingleCallVersions allowlists the SDK protocol versions the
// single-call lane commits. v1alpha3 always carries a page envelope and this
// lane cannot fetch further pages, so it is rejected even without one: a
// paged stream must go through the paged collection path, never here.
var commitClaimSingleCallVersions = map[string]struct{}{
	sdkcollector.ProtocolVersionV1Alpha1: {},
	sdkcollector.ProtocolVersionV1Alpha2: {},
}

// CommitClaimFactsRequest carries one bounded remote fact commit: the fenced
// epoch the claim plane holds, the planned commit target, the caller's own
// component identity (never the echo: it seeds envelope FactIDs and rejects
// a cross-component carrier), an opaque retry key, and the verbatim bounded
// collector result.
//
// IdempotencyKey scopes retries to the epoch for wire symmetry with the
// claim-plane protocol; it binds no commit identity by itself. The generation
// is the only retry key: the same generation replays to a deterministic
// upsert on identity-derived FactIDs, and the boundary's
// generation-finalized skip (errGenerationAlreadyFinalized in storage) turns
// a replayed commit into a nil no-op.
type CommitClaimFactsRequest struct {
	Mutation       workflow.ClaimMutation
	Scope          scope.IngestionScope
	Generation     scope.ScopeGeneration
	ComponentID    string
	IdempotencyKey string
	ResultJSON     []byte
}

// CommitClaimFacts persists one bounded collector result through the
// collector-owned commit boundary without completing the claim. It is the
// commit half of commit-then-complete in one fenced epoch: the caller runs
// this first, then CompleteClaim (state-only, unchanged) under the same
// fencing token.
//
// Internal-only entry point: the sole caller is the core claim-plane commit
// handler (eshu-hq/eshu#8877), which MUST run the admission stack before
// invoking it — validator.ValidateResult against the claim contract, bounded
// statuses, the full returned-claim match, payload-schema validation, and
// producer-grant coverage. This function enforces only the lane-local checks
// below (bounds, strict decode, single-call framing, epoch identity) and the
// committer's own in-transaction fence. It is not a remote handler and must
// never be exposed as one; the wire contract names the admission
// precondition where the server half lands.
//
// The order is fixed: shape and size checks, strict decode, version, page,
// status, and identity checks, envelope mapping, then the fence probe
// immediately before the single committer write. The probe is a HeartbeatClaim,
// which runs the same fenced-update guard family as the Complete path
// (fencing token, owner, and live lease must match or the epoch is rejected):
// a stale fence fails before any byte is committed, so no partial facts can
// land. The committer re-validates the fence inside its own transaction, so
// a lease lost between probe and commit still fails closed.
//
// No new metrics: the fence probe reads on the claim-plane client metrics
// (eshu_dp_workflow_claim_plane_requests_total with
// operation="HeartbeatClaim") only when the store is the plane client; on
// the core path it is a direct store heartbeat with no client-metric read,
// and probe outcomes surface as the handler's error return. The committer
// write bypasses Service.commitWithTelemetry, so
// eshu_dp_facts_emitted_total and eshu_dp_facts_committed_total do NOT fire
// on this path; the wire call itself reads on the same two client metrics
// with operation="CommitClaimFacts".
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
	if strings.TrimSpace(req.ComponentID) == "" {
		return errors.New("commit claim facts: component id is required")
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
	result, err := CheckCommitClaimCarrier(req)
	if err != nil {
		return err
	}
	envelopes, err := mapCommitClaimEnvelopes(req, result)
	if err != nil {
		return err
	}
	// Fence and lease probe immediately before the commit: everything above
	// is pure bytes, so an undecodable or mismatched carrier never extends
	// a dead epoch and starves reclaim. The mutation stamps now (UTC) for
	// the probe: the Postgres heartbeat derives lease_expires_at from the
	// probed ObservedAt, so a claim-time ObservedAt from a satellite that
	// collected for minutes would write an already-past expiry. The stamped
	// mutation also feeds the commit below, keeping probe and write on one
	// lease. The heartbeat extends the lease to cover the commit that
	// follows.
	req.Mutation.ObservedAt = time.Now().UTC()
	if err := store.HeartbeatClaim(ctx, req.Mutation); err != nil {
		return fmt.Errorf("commit claim facts fence probe: %w", err)
	}
	claimed, ok := committer.(ClaimedCommitter)
	if !ok {
		return errors.New("claim-aware collector committer must implement ClaimedCommitter")
	}
	generation := req.Generation
	generation.TrustedDeletion = result.Generation.TrustedDeletion
	if err := claimed.CommitClaimedScopeGeneration(
		ctx,
		req.Mutation,
		req.Scope,
		generation,
		FactsFromSlice(req.Scope, generation, envelopes).Facts,
	); err != nil {
		return fmt.Errorf("commit claim facts commit: %w", err)
	}
	return nil
}

// CheckCommitClaimCarrier runs the lane's pure-bytes carrier check without
// committing: strict-decode the result, then prove it belongs to the fenced
// epoch and fits the single-call framing. The core commit handler
// (extensionhost.CommitHandler) runs this before its own admission stack,
// so the two paths share one decode-and-identity implementation instead of
// a copy that can drift. A carrier failure here is terminal-invalid: retry
// cannot change what the result belongs to.
func CheckCommitClaimCarrier(req CommitClaimFactsRequest) (sdkcollector.Result, error) {
	result, err := decodeCommitClaimResult(req.ResultJSON)
	if err != nil {
		return sdkcollector.Result{}, err
	}
	if err := checkCommitClaimIdentity(req, result); err != nil {
		return sdkcollector.Result{}, err
	}
	return result, nil
}

// decodeCommitClaimResult strict-decodes one collector result: unknown
// fields and trailing data fail closed, so a carrier the producer changed
// under us never commits partially-understood bytes. Numbers stay exact
// through UseNumber, the canonical decode discipline, so large payload
// integers never lose precision through float64.
func decodeCommitClaimResult(raw []byte) (sdkcollector.Result, error) {
	var result sdkcollector.Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return sdkcollector.Result{}, fmt.Errorf("commit claim facts decode result: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return sdkcollector.Result{}, errors.New("commit claim facts decode result: trailing data after JSON document")
	}
	return result, nil
}

// checkCommitClaimIdentity proves the result belongs to the fenced epoch and
// fits the single-call lane: an allowlisted non-paged protocol version, no
// page envelope, a complete state with no statuses (the lane records none,
// so a signaling result must take the full claim path), and the echoed work
// item, generation, fencing token, and component identity matching the
// planned target exactly — never scraped from the echo.
func checkCommitClaimIdentity(req CommitClaimFactsRequest, result sdkcollector.Result) error {
	if _, ok := commitClaimSingleCallVersions[strings.TrimSpace(result.ProtocolVersion)]; !ok {
		return fmt.Errorf(
			"commit claim facts: protocol_version %q is not a single-call version",
			result.ProtocolVersion,
		)
	}
	if result.Page != nil {
		return errors.New("commit claim facts: result carries a page envelope but the lane does not page")
	}
	if result.State != sdkcollector.ResultComplete {
		return fmt.Errorf("commit claim facts: result state %q is not complete", result.State)
	}
	if len(result.Statuses) > 0 {
		return fmt.Errorf("commit claim facts: result carries %d statuses the lane cannot record", len(result.Statuses))
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
	if strings.TrimSpace(result.Claim.ComponentID) != strings.TrimSpace(req.ComponentID) {
		return errors.New("commit claim facts: result component does not match the caller")
	}
	return nil
}

// mapCommitClaimEnvelopes converts SDK facts to commit envelopes through the
// canonical adapter: FactIDs seed from the caller-owned component identity,
// CollectorKind comes from the planned target (never the echo), ObservedAt is
// UTC-normalized, and (kind, stableKey) duplicates collapse exactly like the
// in-tree path. Scope and generation come from the fenced target, never the
// source echo, and every fact's echoed refs must name that same target: facts
// for another scope or generation fail closed before the stream opens.
// FactID derives deterministically from the fact identity so a replayed
// commit upserts the same rows instead of duplicating them.
func mapCommitClaimEnvelopes(req CommitClaimFactsRequest, result sdkcollector.Result) ([]facts.Envelope, error) {
	envelopes := make([]facts.Envelope, 0, len(result.Facts))
	seen := make(map[string]struct{}, len(result.Facts))
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
		key := fact.Kind + "\x00" + fact.StableKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		envelopes = append(envelopes, factenvelope.InternalFromSDKFact(fact, factenvelope.InternalEnvelopeOptions{
			ComponentID:   strings.TrimSpace(req.ComponentID),
			ScopeID:       req.Scope.ScopeID,
			GenerationID:  req.Generation.GenerationID,
			CollectorKind: string(req.Scope.CollectorKind),
			FencingToken:  req.Mutation.FencingToken,
		}))
	}
	return envelopes, nil
}
