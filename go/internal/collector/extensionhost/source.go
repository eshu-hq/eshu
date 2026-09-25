// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package extensionhost adapts public collector SDK results into the core
// claim-aware collector commit boundary.
package extensionhost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	sdkconformance "github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// NewSource validates component host configuration and builds a claimed source.
// Core-owned fact-kind declarations require live producer grants from
// Config.Grants; without them construction fails exactly as grant-less
// manifest validation rejects them.
func NewSource(config Config) (*Source, error) {
	// The activation gate is the owning decision site for the activation-stage
	// grant signal: report the manifest's core-kind decisions once, whether or
	// not the grant allows, then fail closed on a validation error.
	config.Manifest.ObserveManifestGrants(
		context.Background(), config.GrantObserver, component.GrantStageActivation, config.Grants, time.Now().UTC(),
	)
	if err := config.Manifest.ValidateWithGrants(config.Grants); err != nil {
		return nil, fmt.Errorf("validate component manifest: %w", err)
	}
	instanceID := strings.TrimSpace(config.CollectorInstanceID)
	if instanceID == "" {
		return nil, errors.New("collector instance id is required")
	}
	if strings.TrimSpace(string(config.ScopeKind)) == "" {
		return nil, errors.New("scope kind is required")
	}
	if strings.TrimSpace(config.ConfigHandle) == "" {
		return nil, errors.New("config handle is required")
	}
	if config.Runner == nil {
		return nil, errors.New("extension runner is required")
	}
	contract, err := sdkContract(config.Manifest)
	if err != nil {
		return nil, err
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	grants := append([]component.ProducerGrant(nil), config.Grants...)
	liveGrants := config.LiveGrants
	if liveGrants == nil {
		liveGrants = func() ([]component.ProducerGrant, error) { return grants, nil }
	}
	collectorKinds := make(map[scope.CollectorKind]struct{}, len(config.Manifest.Spec.CollectorKinds))
	for _, kind := range config.Manifest.Spec.CollectorKinds {
		collectorKinds[scope.CollectorKind(kind)] = struct{}{}
	}
	return &Source{
		manifest:            config.Manifest,
		collectorInstanceID: instanceID,
		scopeKind:           config.ScopeKind,
		configHandle:        strings.TrimSpace(config.ConfigHandle),
		config:              cloneConfig(config.Config),
		contract:            contract,
		validator:           sdkcollector.NewValidator(contract),
		payloadSchemas:      payloadSchemasForManifest(config.Manifest),
		runner:              config.Runner,
		statusRecorder:      config.StatusRecorder,
		clock:               clock,
		collectorKinds:      collectorKinds,
		liveGrants:          liveGrants,
		grantObserver:       config.GrantObserver,
	}, nil
}

// NextClaimed runs one extension claim and returns facts for ClaimedService.
func (s *Source) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if err := s.validateWorkItem(item); err != nil {
		return collector.CollectedGeneration{}, false, extensionFailure{
			class:    FailureClassInvalidClaim,
			terminal: true,
			cause:    err,
		}
	}
	request := s.requestForWorkItem(item)
	runCtx, cancel := context.WithDeadline(ctx, request.Claim.Deadline)
	defer cancel()

	result, err := s.runner.RunCollector(runCtx, request)
	if err != nil {
		return collector.CollectedGeneration{}, false, extensionFailure{
			class: FailureClassLaunchFailure,
			cause: err,
		}
	}
	if err := s.validateResult(ctx, request, result); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if err := s.recordStatuses(ctx, item, result); err != nil {
		return collector.CollectedGeneration{}, false, extensionFailure{
			class: FailureClassStatusRecord,
			cause: err,
		}
	}

	switch result.State {
	case sdkcollector.ResultRetryable:
		return collector.CollectedGeneration{}, false, extensionFailure{
			class: failureClassFromStatus(result, "retryable"),
			cause: errors.New("extension requested retryable result"),
		}
	case sdkcollector.ResultTerminal:
		return collector.CollectedGeneration{}, false, extensionFailure{
			class:    failureClassFromStatus(result, "terminal"),
			terminal: true,
			cause:    errors.New("extension requested terminal result"),
		}
	case sdkcollector.ResultUnchanged:
		collected := s.collectedGeneration(item, result, nil)
		collected.Unchanged = true
		return collected, true, nil
	default:
		envelopes := s.envelopesForResult(item, result)
		return s.collectedGeneration(item, result, envelopes), true, nil
	}
}

func (s *Source) validateWorkItem(item workflow.WorkItem) error {
	if err := item.Validate(); err != nil {
		return err
	}
	if item.Status != workflow.WorkItemStatusClaimed {
		return fmt.Errorf("work item status %q must be claimed", item.Status)
	}
	if item.CollectorInstanceID != s.collectorInstanceID {
		return fmt.Errorf(
			"work item collector_instance_id %q does not match extension instance %q",
			item.CollectorInstanceID,
			s.collectorInstanceID,
		)
	}
	if _, ok := s.collectorKinds[item.CollectorKind]; !ok {
		return fmt.Errorf("component %q cannot collect kind %q", s.manifest.Metadata.ID, item.CollectorKind)
	}
	if item.AttemptCount < 1 {
		return fmt.Errorf("attempt_count must be >= 1 after claim")
	}
	if item.CurrentFencingToken <= 0 {
		return fmt.Errorf("current_fencing_token must be positive")
	}
	if item.LeaseExpiresAt.IsZero() {
		return fmt.Errorf("lease_expires_at must not be zero")
	}
	return nil
}

func (s *Source) validateResult(ctx context.Context, request Request, result sdkcollector.Result) error {
	if _, err := s.validator.ValidateResult(result); err != nil {
		return extensionFailure{
			class:    FailureClassInvalidResult,
			terminal: true,
			cause:    err,
		}
	}
	if err := validateBoundedStatuses(result); err != nil {
		return extensionFailure{
			class:    FailureClassInvalidResult,
			terminal: true,
			cause:    err,
		}
	}
	if err := validateReturnedClaim(request.Claim, result.Claim); err != nil {
		return extensionFailure{
			class:    FailureClassIdentityMismatch,
			terminal: true,
			cause:    err,
		}
	}
	if err := s.validatePayloadSchemas(result); err != nil {
		return extensionFailure{
			class:    FailureClassInvalidResult,
			terminal: true,
			cause:    err,
		}
	}
	if err := s.validateGrantCoverage(ctx, result); err != nil {
		return extensionFailure{
			class:    FailureClassInvalidResult,
			terminal: true,
			cause:    err,
		}
	}
	return nil
}

// validateGrantCoverage rechecks core-issued producer authorization on every
// emission against the live grant set, so a grant revoked after activation
// fails the next result closed instead of emitting under a dead
// authorization. Kinds that are not core-owned need no grant. An unreadable
// grant set denies core-owned kinds (fail closed) and is reported as such.
//
// It is also the owning decision site for the emission-stage grant signal
// (#6726): one decision is reported per distinct core-owned kind in the
// result on allow, and one on the first deny (which returns), so a result is
// never counted twice. The hot path adds only a nil check when no observer is
// configured and no core lookup beyond the authorization it already ran.
func (s *Source) validateGrantCoverage(ctx context.Context, result sdkcollector.Result) error {
	grants, readErr := s.liveGrants()
	if readErr != nil {
		grants = nil
	}
	now := s.clock()
	var seenBuf [4]string
	seen := seenBuf[:0]
	for _, fact := range result.Facts {
		allowed, governed := component.EvaluateEmission(
			grants,
			s.manifest.Metadata.ID,
			s.manifest.Metadata.Version,
			fact.Kind,
			fact.SchemaVersion,
			s.manifest.Spec.CollectorKinds,
			now,
		)
		if allowed {
			if governed && s.grantObserver != nil {
				seen = s.observeEmissionAllow(ctx, fact.Kind, seen)
			}
			continue
		}
		if s.grantObserver != nil {
			s.observeEmissionDeny(ctx, grants, readErr, fact, now)
		}
		return fmt.Errorf(
			"fact kind %q is core-owned by Eshu and has no live producer grant for %q version %q",
			fact.Kind, s.manifest.Metadata.ID, s.manifest.Metadata.Version,
		)
	}
	return nil
}

// observeEmissionAllow reports one allow per distinct kind, tracking reported
// kinds in seen (a stack-backed slice for the common few-kind result).
func (s *Source) observeEmissionAllow(ctx context.Context, kind string, seen []string) []string {
	trimmed := strings.TrimSpace(kind)
	for _, reported := range seen {
		if reported == trimmed {
			return seen
		}
	}
	s.grantObserver.ObserveGrantDecision(ctx, component.GrantDecision{
		Stage:      component.GrantStageEmission,
		Allowed:    true,
		Reason:     component.GrantReasonGranted,
		ProducerID: s.manifest.Metadata.ID,
		Version:    s.manifest.Metadata.Version,
		Kind:       trimmed,
	})
	return append(seen, trimmed)
}

// observeEmissionDeny reports the deny for the fact that failed the recheck.
// Classification runs only here, on the cold deny path.
func (s *Source) observeEmissionDeny(
	ctx context.Context,
	grants []component.ProducerGrant,
	readErr error,
	fact sdkcollector.Fact,
	now time.Time,
) {
	reason := component.GrantReasonGrantsUnreadable
	if readErr == nil {
		reason = component.ClassifyEmission(
			grants,
			s.manifest.Metadata.ID,
			s.manifest.Metadata.Version,
			fact.Kind,
			fact.SchemaVersion,
			s.manifest.Spec.CollectorKinds,
			now,
		)
	}
	s.grantObserver.ObserveGrantDecision(ctx, component.GrantDecision{
		Stage:      component.GrantStageEmission,
		Allowed:    false,
		Reason:     reason,
		ProducerID: s.manifest.Metadata.ID,
		Version:    s.manifest.Metadata.Version,
		Kind:       strings.TrimSpace(fact.Kind),
	})
}

func (s *Source) validatePayloadSchemas(result sdkcollector.Result) error {
	if len(s.payloadSchemas) == 0 {
		return nil
	}
	if err := sdkconformance.ValidatePayloadSchemas(s.payloadSchemas, result); err != nil {
		return fmt.Errorf("%s: %w", sdkconformance.FindingPayloadSchemaInvalid, err)
	}
	return nil
}
