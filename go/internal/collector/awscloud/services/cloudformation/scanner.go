// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS CloudFormation metadata facts for one claimed account and
// region.
//
// CloudFormation is the highest template-body redaction surface in the AWS
// collector. The scanner never reads a template body (GetTemplate,
// GetTemplateSummary), never reads parameter values, never reads change-set
// bodies, and never persists drift property documents or secret-like stack
// output values. The forbidden APIs are excluded from the Client interface by
// construction; see TestClientInterfaceExcludesMutationAndTemplateAPIs.
type Scanner struct {
	Client Client
	// RedactionKey produces deterministic redaction markers for secret-like
	// stack output values. The runtimebind builder requires a non-zero key.
	RedactionKey redact.Key
}

// Scan observes CloudFormation stacks, stack sets, stack instances, change
// sets, drift results, and registered types through the configured client and
// returns reported-confidence AWS facts. The scan only reaches read-only
// list-and-describe paths and never reaches template-body, parameter-value,
// change-set-body, or mutation surfaces.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cloudformation scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("cloudformation scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCloudFormation:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCloudFormation
	default:
		return nil, fmt.Errorf("cloudformation scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	stacks, err := s.Client.ListStacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudFormation stacks: %w", err)
	}
	for _, stack := range stacks {
		envelopes, err = s.appendStack(ctx, envelopes, boundary, stack)
		if err != nil {
			return nil, err
		}
	}

	stackSets, err := s.Client.ListStackSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudFormation stack sets: %w", err)
	}
	for _, stackSet := range stackSets {
		envelopes, err = s.appendStackSet(ctx, envelopes, boundary, stackSet)
		if err != nil {
			return nil, err
		}
	}

	types, err := s.Client.ListTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudFormation types: %w", err)
	}
	for _, registeredType := range types {
		envelope, err := awscloud.NewResourceEnvelope(typeObservation(boundary, registeredType))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func (s Scanner) appendStack(
	ctx context.Context,
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	stack Stack,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(stackObservation(boundary, stack, s.RedactionKey))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)

	for _, relationship := range stackRelationships(boundary, stack) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	// Deleted stacks no longer carry resources, change sets, or drift results
	// addressable by the read APIs, so the scanner only enriches live stacks.
	if stack.Deleted {
		return envelopes, nil
	}

	stackKey := firstNonEmpty(stack.ID, stack.Name)
	resources, err := s.Client.ListStackResources(ctx, stackKey)
	if err != nil {
		return nil, fmt.Errorf("list stack resources for %q: %w", stackKey, err)
	}
	for _, relationship := range stackResourceTypeRelationships(boundary, stack, resources) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	changeSets, err := s.Client.ListChangeSets(ctx, stackKey)
	if err != nil {
		return nil, fmt.Errorf("list change sets for %q: %w", stackKey, err)
	}
	for _, changeSet := range changeSets {
		envelope, err := awscloud.NewResourceEnvelope(changeSetObservation(boundary, changeSet))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	drift, err := s.Client.ListStackResourceDrifts(ctx, stackKey)
	if err != nil {
		return nil, fmt.Errorf("list drift results for %q: %w", stackKey, err)
	}
	if drift.TotalChecked > 0 {
		envelope, err := awscloud.NewResourceEnvelope(driftObservation(boundary, stack, drift))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func (s Scanner) appendStackSet(
	ctx context.Context,
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	stackSet StackSet,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(stackSetObservation(boundary, stackSet))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)

	instances, err := s.Client.ListStackInstances(ctx, firstNonEmpty(stackSet.Name, stackSet.ID))
	if err != nil {
		return nil, fmt.Errorf("list stack instances for %q: %w", stackSet.Name, err)
	}
	for _, instance := range instances {
		envelope, err := awscloud.NewResourceEnvelope(stackInstanceObservation(boundary, stackSet, instance))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)

		if relationship, ok := stackSetInstanceRelationship(boundary, stackSet, instance); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}
	return envelopes, nil
}
