// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	sdkconformance "github.com/eshu-hq/eshu-sdk/collector/conformance"
	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// readCommitBody reads the raw envelope bounded by the shared body cap. The
// proxy enforces the same cap before forwarding; core re-enforces it here
// because the proxy is never trusted with the bound.
func readCommitBody(writer http.ResponseWriter, request *http.Request) []byte {
	request.Body = http.MaxBytesReader(writer, request.Body, workflowv1.MaxCommitBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		writeCommitError(writer, http.StatusBadRequest, workflowv1.CodeInvalid,
			fmt.Sprintf("decode CommitClaimFacts request: %v", err))
		return nil
	}
	return raw
}

// wireMutationToMutation converts the wire mutation to the internal shape
// and stamps the planned IDs the wire carries beside the result. Core never
// scrapes its commit target out of the echo: the planned IDs travel beside
// it, and the echo is checked against them in admitResult. The wire carries
// no derived-scanner flag, so the internal opt-in stays false and the fence
// refuses derived claims on this path fail-closed.
func wireMutationToMutation(wire workflowv1.CommitClaimFactsRequest) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:           strings.TrimSpace(wire.WorkItemID),
		ClaimID:              strings.TrimSpace(wire.ClaimID),
		FencingToken:         wire.FencingToken,
		OwnerID:              strings.TrimSpace(wire.OwnerID),
		ObservedAt:           wire.ObservedAt.UTC(),
		LeaseDuration:        wire.LeaseDuration,
		TenantID:             strings.TrimSpace(wire.TenantID),
		WorkspaceID:          strings.TrimSpace(wire.WorkspaceID),
		SubjectClass:         strings.TrimSpace(wire.SubjectClass),
		PolicyRevisionHash:   strings.TrimSpace(wire.PolicyRevisionHash),
		ScopeID:              strings.TrimSpace(wire.ScopeID),
		SourceRunID:          strings.TrimSpace(wire.SourceRunID),
		GenerationID:         strings.TrimSpace(wire.GenerationID),
		FailureClass:         strings.TrimSpace(wire.FailureClass),
		FailureMessage:       strings.TrimSpace(wire.FailureMessage),
		FailureOperation:     strings.TrimSpace(wire.FailureOperation),
		FailureStatusCode:    wire.FailureStatusCode,
		FailureTerminal:      wire.FailureTerminal,
		VisibleAt:            wire.VisibleAt.UTC(),
		ResolvedScopeID:      strings.TrimSpace(wire.ResolvedScopeID),
		ResolvedSourceRunID:  strings.TrimSpace(wire.ResolvedSourceRunID),
		ResolvedGenerationID: strings.TrimSpace(wire.ResolvedGenerationID),
	}
}

// admitResult runs the full admission stack before any fence probe or
// commit: the SDK contract validator, bounded statuses with the single-call
// lane's no-statuses disposition, the echo-vs-planned identity match, the
// payload schemas, and the producer-grant recheck. The carrier check runs
// first so the handler and the lane share one decode-and-identity
// implementation; the extended match below is the second, independent check
// against the handler's configured producer identity. A refusal here never
// touches the epoch, so a malformed carrier cannot extend a dead lease and
// starve reclaim.
func (h *CommitHandler) admitResult(mutation workflow.ClaimMutation, rawResult json.RawMessage) (sdkcollector.Result, error) {
	carrier, err := collector.CheckCommitClaimCarrier(collector.CommitClaimFactsRequest{
		Mutation:    mutation,
		Generation:  scope.ScopeGeneration{GenerationID: mutation.GenerationID},
		ComponentID: h.componentID,
		ResultJSON:  []byte(rawResult),
	})
	if err != nil {
		return sdkcollector.Result{}, &workflowv1.ProtocolError{Code: workflowv1.CodeInvalid, Message: fmt.Sprintf("invalid carrier: %v", err)}
	}
	result := carrier
	if _, err := h.validator.ValidateResult(result); err != nil {
		return sdkcollector.Result{}, &workflowv1.ProtocolError{Code: workflowv1.CodeInvalid, Message: fmt.Sprintf("invalid result: %v", err)}
	}
	if err := validateBoundedStatuses(result); err != nil {
		return sdkcollector.Result{}, &workflowv1.ProtocolError{Code: workflowv1.CodeInvalid, Message: fmt.Sprintf("invalid result: %v", err)}
	}
	if err := h.matchReturnedClaim(mutation, result); err != nil {
		return sdkcollector.Result{}, &workflowv1.ProtocolError{Code: workflowv1.CodeInvalid, Message: err.Error()}
	}
	if len(h.payloadSchemas) > 0 {
		if err := sdkconformance.ValidatePayloadSchemas(h.payloadSchemas, result); err != nil {
			return sdkcollector.Result{}, &workflowv1.ProtocolError{Code: workflowv1.CodeInvalid, Message: fmt.Sprintf("invalid result payload: %v", err)}
		}
	}
	if err := h.checkGrantCoverage(result); err != nil {
		return sdkcollector.Result{}, &workflowv1.ProtocolError{Code: workflowv1.CodeInvalid, Message: err.Error()}
	}
	return result, nil
}

// matchReturnedClaim proves the result echo names the planned epoch and the
// configured producer: work item, generation, source run, scope, fencing
// token, and the component, instance, kind, and source system this handler
// serves. Anything else is a terminal identity failure, never a retryable
// one: retry cannot change who the result belongs to.
func (h *CommitHandler) matchReturnedClaim(mutation workflow.ClaimMutation, result sdkcollector.Result) error {
	echo := result.Claim
	mismatch := func(field string) error {
		return fmt.Errorf("result claim %s does not match the fenced epoch", field)
	}
	if strings.TrimSpace(echo.WorkItemID) != strings.TrimSpace(mutation.WorkItemID) {
		return mismatch("work item")
	}
	if strings.TrimSpace(result.Generation.ID) != strings.TrimSpace(mutation.GenerationID) {
		return mismatch("generation")
	}
	if strings.TrimSpace(echo.SourceRunID) != strings.TrimSpace(mutation.SourceRunID) {
		return mismatch("source run")
	}
	if strings.TrimSpace(echo.Scope.ID) != strings.TrimSpace(mutation.ScopeID) {
		return mismatch("scope")
	}
	echoed, err := strconv.ParseInt(strings.TrimSpace(echo.FencingToken), 10, 64)
	if err != nil {
		return errors.New("result claim fencing token is not a base-10 integer")
	}
	if echoed != mutation.FencingToken {
		return mismatch("fencing token")
	}
	if strings.TrimSpace(echo.ComponentID) != h.componentID {
		return mismatch("component")
	}
	if strings.TrimSpace(echo.InstanceID) != h.instanceID {
		return mismatch("instance")
	}
	if _, ok := h.collectorKinds[strings.TrimSpace(echo.CollectorKind)]; !ok {
		return fmt.Errorf("result claim kind %q is not served by this handler", echo.CollectorKind)
	}
	if strings.TrimSpace(string(h.scopeKind)) != "" && strings.TrimSpace(echo.Scope.Kind) != "" &&
		strings.TrimSpace(echo.Scope.Kind) != strings.TrimSpace(string(h.scopeKind)) {
		return mismatch("scope kind")
	}
	return nil
}

// buildTarget builds the commit scope and generation for one admitted
// result. The kind gate runs first: a kind the manifest never declared fails
// closed instead of committing under a fabricated scope. The partition falls
// back to source-system:scope-id exactly like the in-tree mapper, because
// the wire carries no fairness key; TestCommitHandlerScopeMirrorsInTreeMapping
// pins the two builders identical for that shape.
func (h *CommitHandler) buildTarget(result sdkcollector.Result) (scope.IngestionScope, scope.ScopeGeneration, error) {
	echo := result.Claim
	kind := strings.TrimSpace(echo.CollectorKind)
	if _, ok := h.collectorKinds[kind]; !ok {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, fmt.Errorf("result kind %q is not served by this handler", echo.CollectorKind)
	}
	observedAt := result.Generation.ObservedAt.UTC()
	ingestedAt := h.clock().UTC()
	if ingestedAt.Before(observedAt) {
		ingestedAt = observedAt
	}
	scopeValue := scope.IngestionScope{
		ScopeID:       strings.TrimSpace(echo.Scope.ID),
		SourceSystem:  strings.TrimSpace(echo.SourceSystem),
		ScopeKind:     h.scopeKind,
		CollectorKind: scope.CollectorKind(kind),
		PartitionKey:  strings.TrimSpace(echo.SourceSystem) + ":" + strings.TrimSpace(echo.Scope.ID),
		Metadata: map[string]string{
			"component_id": h.componentID,
			"instance_id":  h.instanceID,
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID:    strings.TrimSpace(result.Generation.ID),
		ScopeID:         strings.TrimSpace(echo.Scope.ID),
		ObservedAt:      observedAt,
		IngestedAt:      ingestedAt,
		Status:          scope.GenerationStatusPending,
		TriggerKind:     scope.TriggerKindSnapshot,
		FreshnessHint:   result.Generation.FreshnessHint,
		TrustedDeletion: result.Generation.TrustedDeletion,
	}
	return scopeValue, generation, nil
}

// checkGrantCoverage rechecks core-issued producer authorization against the
// live grant set, so a grant revoked after activation fails the next result
// closed instead of committing under a dead authorization. Kinds that are
// not core-owned need no grant. An unreadable grant set denies core-owned
// kinds fail-closed.
func (h *CommitHandler) checkGrantCoverage(result sdkcollector.Result) error {
	grants, readErr := h.liveGrants()
	if readErr != nil {
		grants = nil
	}
	now := h.clock()
	for _, fact := range result.Facts {
		allowed, governed := component.EvaluateEmission(
			grants,
			h.manifest.Metadata.ID,
			h.manifest.Metadata.Version,
			fact.Kind,
			fact.SchemaVersion,
			h.manifest.Spec.CollectorKinds,
			now,
		)
		if allowed || !governed {
			continue
		}
		if readErr != nil {
			return fmt.Errorf("producer grants are unreadable, denying core-owned kind %q", strings.TrimSpace(fact.Kind))
		}
		return fmt.Errorf(
			"fact kind %q is core-owned by Eshu and has no live producer grant for %q version %q",
			strings.TrimSpace(fact.Kind), h.manifest.Metadata.ID, h.manifest.Metadata.Version)
	}
	return nil
}

// commitContext bounds one commit under the handler timeout, keeping the
// request's earlier deadline when the caller already set one. An overrun
// cancels the commit and answers 500: the carrier stays unacknowledged so
// the proxy retries instead of completing.
func (h *CommitHandler) commitContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := h.commitTimeout
	if deadline, ok := parent.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(parent, timeout)
}
