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
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// NewSource validates component host configuration and builds a claimed source.
func NewSource(config Config) (*Source, error) {
	if err := config.Manifest.Validate(); err != nil {
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
		runner:              config.Runner,
		statusRecorder:      config.StatusRecorder,
		clock:               clock,
		collectorKinds:      collectorKinds,
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
	if err := s.validateResult(request, result); err != nil {
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

func (s *Source) validateResult(request Request, result sdkcollector.Result) error {
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
	return nil
}
