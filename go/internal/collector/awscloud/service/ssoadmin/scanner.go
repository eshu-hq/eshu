// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssoadmin

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS IAM Identity Center metadata facts for one claimed
// org-scoped account. It never calls mutation APIs, never reads permission set
// inline policy bodies, and never persists application access-scope filters.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes Identity Center instances, permission sets, account
// assignments, applications, trusted token issuers, and resolved principals
// through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ssoadmin scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("ssoadmin scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSSOAdmin:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceSSOAdmin
	default:
		return nil, fmt.Errorf("ssoadmin scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot AWS IAM Identity Center metadata: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, boundary, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, instance := range snapshot.Instances {
		if err := s.appendInstance(&envelopes, boundary, instance); err != nil {
			return nil, err
		}
	}
	for _, application := range snapshot.Applications {
		if err := appendApplication(&envelopes, boundary, application); err != nil {
			return nil, err
		}
	}
	for _, principal := range snapshot.Principals {
		if err := s.appendPrincipal(&envelopes, boundary, principal); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func (s Scanner) appendInstance(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	instance Instance,
) error {
	if err := appendResource(envelopes, instanceObservation(boundary, instance)); err != nil {
		return err
	}
	for _, permSet := range instance.PermissionSets {
		if err := appendPermissionSet(envelopes, boundary, instance, permSet); err != nil {
			return err
		}
	}
	for _, assignment := range instance.AccountAssignments {
		if err := appendAssignment(envelopes, boundary, assignment); err != nil {
			return err
		}
	}
	for _, issuer := range instance.TrustedTokenIssuers {
		if err := appendResource(envelopes, trustedTokenIssuerObservation(boundary, issuer)); err != nil {
			return err
		}
	}
	return nil
}

func appendPermissionSet(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	instance Instance,
	permSet PermissionSet,
) error {
	if err := appendResource(envelopes, permissionSetObservation(boundary, permSet)); err != nil {
		return err
	}
	if rel, ok := permissionSetInInstanceRelationship(boundary, instance, permSet); ok {
		if err := appendRelationship(envelopes, rel); err != nil {
			return err
		}
	}
	for _, managed := range permSet.ManagedPolicies {
		if rel, ok := managedPolicyRelationship(boundary, permSet, managed); ok {
			if err := appendRelationship(envelopes, rel); err != nil {
				return err
			}
		}
	}
	for _, customer := range permSet.CustomerManagedPolicies {
		if rel, ok := customerManagedPolicyRelationship(boundary, permSet, customer); ok {
			if err := appendRelationship(envelopes, rel); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendAssignment(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	assignment AccountAssignment,
) error {
	if err := appendResource(envelopes, assignmentObservation(boundary, assignment)); err != nil {
		return err
	}
	for _, build := range []func(awscloud.Boundary, AccountAssignment) (awscloud.RelationshipObservation, bool){
		assignmentUsesPermissionSetRelationship,
		assignmentTargetsAccountRelationship,
		assignmentGrantsPrincipalRelationship,
	} {
		rel, ok := build(boundary, assignment)
		if !ok {
			continue
		}
		if err := appendRelationship(envelopes, rel); err != nil {
			return err
		}
	}
	return nil
}

func appendApplication(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	application Application,
) error {
	if err := appendResource(envelopes, applicationObservation(boundary, application)); err != nil {
		return err
	}
	if rel, ok := applicationInInstanceRelationship(boundary, application); ok {
		if err := appendRelationship(envelopes, rel); err != nil {
			return err
		}
	}
	return nil
}

func (s Scanner) appendPrincipal(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	principal Principal,
) error {
	return appendResource(envelopes, s.principalObservation(boundary, principal))
}

func appendResource(envelopes *[]facts.Envelope, observation awscloud.ResourceObservation) error {
	envelope, err := awscloud.NewResourceEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendRelationship(envelopes *[]facts.Envelope, observation awscloud.RelationshipObservation) error {
	envelope, err := awscloud.NewRelationshipEnvelope(observation)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func appendWarnings(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	warnings []awscloud.WarningObservation,
) error {
	for _, warning := range warnings {
		warning.Boundary = boundary
		envelope, err := awscloud.NewWarningEnvelope(warning)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
