// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shield

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Shield Advanced metadata facts for one claimed account. It
// observes protections and the account subscription summary, never reads
// billing detail beyond the subscription state and auto-renew flag, and never
// calls any Shield mutation API.
type Scanner struct {
	Client Client
}

// Scan observes Shield Advanced protections and the account subscription
// summary through the configured client. Shield is a global service, so one
// claim per account observes every protection regardless of region; the
// scanner records the reported boundary on each resource. Each protection
// emits a resource fact plus a protection-to-protected-resource relationship
// when the protected ARN resolves to a known Eshu resource family.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("shield scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceShield:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceShield
	default:
		return nil, fmt.Errorf("shield scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	protectionEnvelopes, err := s.scanProtections(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, protectionEnvelopes...)

	subscriptionEnvelopes, err := s.scanSubscription(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, subscriptionEnvelopes...)

	return envelopes, nil
}

func (s Scanner) scanProtections(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	protections, err := s.Client.ListProtections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Shield protections: %w", err)
	}
	var envelopes []facts.Envelope
	for _, protection := range protections {
		resource, err := awscloud.NewResourceEnvelope(protectionObservation(boundary, protection))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		if relationship := protectionRelationship(boundary, protection); relationship != nil {
			envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func (s Scanner) scanSubscription(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	subscription, err := s.Client.DescribeSubscription(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Shield subscription: %w", err)
	}
	if subscription == nil {
		return nil, nil
	}
	resource, err := awscloud.NewResourceEnvelope(subscriptionObservation(boundary, *subscription))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

func protectionObservation(boundary awscloud.Boundary, protection Protection) awscloud.ResourceObservation {
	arn := strings.TrimSpace(protection.ARN)
	resourceID := firstNonEmpty(arn, protection.ID, protection.Name)
	protectedARN := strings.TrimSpace(protection.ResourceARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeShieldProtection,
		Name:         strings.TrimSpace(protection.Name),
		Attributes: map[string]any{
			"id":                     strings.TrimSpace(protection.ID),
			"protected_resource_arn": protectedARN,
		},
		CorrelationAnchors: []string{arn, protection.ID, protectedARN},
		SourceRecordID:     resourceID,
	}
}

func subscriptionObservation(boundary awscloud.Boundary, subscription Subscription) awscloud.ResourceObservation {
	arn := strings.TrimSpace(subscription.ARN)
	state := strings.TrimSpace(subscription.State)
	resourceID := firstNonEmpty(arn, subscriptionResourceID(boundary))
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeShieldSubscription,
		State:        state,
		Attributes: map[string]any{
			"state":      state,
			"auto_renew": strings.TrimSpace(subscription.AutoRenew),
		},
		CorrelationAnchors: []string{arn, resourceID},
		SourceRecordID:     resourceID,
	}
}

// subscriptionResourceID builds a stable id for the account subscription when
// AWS does not report a subscription ARN. Shield Advanced has exactly one
// subscription per account, so the account id is a stable, unique identity.
func subscriptionResourceID(boundary awscloud.Boundary) string {
	account := strings.TrimSpace(boundary.AccountID)
	if account == "" {
		return "shield-subscription"
	}
	return "shield-subscription/" + account
}
