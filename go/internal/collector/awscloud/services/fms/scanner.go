// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fms

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Firewall Manager metadata facts for one claimed FMS
// administrator account. It never persists policy rule payloads (the
// SecurityServicePolicyData managed service data document) and never calls any
// FMS mutation API.
type Scanner struct {
	Client Client
}

// Scan observes Firewall Manager policies through the configured client and
// emits one resource fact per policy plus one relationship fact per
// Organizations member account the policy applies to. FMS is an
// organization-wide control plane reachable only from the administrator
// account, so the boundary scopes the administrator account's policy fleet.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("fms scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceFMS:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceFMS
	default:
		return nil, fmt.Errorf("fms scanner received service_kind %q", boundary.ServiceKind)
	}

	policies, err := s.Client.ListPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AWS Firewall Manager policies: %w", err)
	}

	var envelopes []facts.Envelope
	for _, policy := range policies {
		resource, err := awscloud.NewResourceEnvelope(policyObservation(boundary, policy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		relationships, err := s.policyMemberAccountRelationships(ctx, boundary, policy)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

// policyMemberAccountRelationships resolves the Organizations member accounts a
// policy is evaluated against and emits one applies-to-account edge per account.
// The member account list is resolved per policy id, deduplicated, and sorted so
// the synthesized relationship identity never keys on API response order.
func (s Scanner) policyMemberAccountRelationships(
	ctx context.Context,
	boundary awscloud.Boundary,
	policy Policy,
) ([]facts.Envelope, error) {
	policyID := strings.TrimSpace(policy.ID)
	if policyID == "" {
		return nil, nil
	}
	accounts, err := s.Client.ListPolicyMemberAccounts(ctx, policyID)
	if err != nil {
		return nil, fmt.Errorf("list AWS Firewall Manager policy %q member accounts: %w", policyID, err)
	}
	var envelopes []facts.Envelope
	for _, accountID := range sortedUnique(accounts) {
		relationship, ok := policyMemberAccountRelationship(boundary, policy, accountID)
		if !ok {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// policyObservation builds the aws_fms_policy resource observation for one
// Firewall Manager policy. The security service type the policy governs and the
// in-scope resource type are recorded as labels; the policy rule payload is
// never read or stored.
func policyObservation(boundary awscloud.Boundary, policy Policy) awscloud.ResourceObservation {
	arn := strings.TrimSpace(policy.ARN)
	policyID := strings.TrimSpace(policy.ID)
	resourceID := firstNonEmpty(arn, policyID, policy.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeFMSPolicy,
		Name:         strings.TrimSpace(policy.Name),
		State:        strings.TrimSpace(policy.PolicyStatus),
		Attributes: map[string]any{
			"policy_id":                          policyID,
			"security_service_type":              strings.TrimSpace(policy.SecurityServiceType),
			"managed_resource_type":              strings.TrimSpace(policy.ResourceType),
			"remediation_enabled":                policy.RemediationEnabled,
			"delete_unused_fm_managed_resources": policy.DeleteUnusedFMManagedResources,
			"policy_status":                      strings.TrimSpace(policy.PolicyStatus),
		},
		CorrelationAnchors: []string{arn, policyID, policy.Name},
		SourceRecordID:     resourceID,
	}
}
