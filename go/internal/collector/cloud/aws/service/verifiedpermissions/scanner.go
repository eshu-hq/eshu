// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedpermissions

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Verified Permissions metadata-only facts for one claimed
// account and region. It never reads or persists Cedar policy statement bodies,
// schema bodies, policy template bodies, or authorization-request payloads, and
// never mutates Verified Permissions state. It reports policy stores, policies,
// and identity sources plus the policy-in-store, identity-source-in-store, and
// identity-source-to-Cognito-user-pool relationships.
type Scanner struct {
	// Client is the metadata-only Verified Permissions snapshot source.
	Client Client
}

// Scan observes Verified Permissions policy stores, their policies and identity
// sources, and the Cognito user pool dependency metadata through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("verifiedpermissions scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceVerifiedPermissions:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceVerifiedPermissions
	default:
		return nil, fmt.Errorf("verifiedpermissions scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Verified Permissions policy stores: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, store := range snapshot.PolicyStores {
		next, err := policyStoreEnvelopes(boundary, store)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func policyStoreEnvelopes(boundary aws.Boundary, store PolicyStore) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(policyStoreObservation(boundary, store))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	storeID := policyStoreResourceID(store)
	for _, policy := range store.Policies {
		next, err := policyEnvelopes(boundary, storeID, policy)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, source := range store.IdentitySources {
		next, err := identitySourceEnvelopes(boundary, storeID, source)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func policyEnvelopes(boundary aws.Boundary, storeID string, policy Policy) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(policyObservation(boundary, policy))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := policyInStoreRelationship(boundary, storeID, policy); relationship != nil {
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func identitySourceEnvelopes(boundary aws.Boundary, storeID string, source IdentitySource) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(identitySourceObservation(boundary, source))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*aws.RelationshipObservation{
		identitySourceInStoreRelationship(boundary, storeID, source),
		identitySourceCognitoRelationship(boundary, source),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func policyStoreObservation(boundary aws.Boundary, store PolicyStore) aws.ResourceObservation {
	storeARN := strings.TrimSpace(store.ARN)
	storeID := strings.TrimSpace(store.ID)
	resourceID := policyStoreResourceID(store)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          storeARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeVerifiedPermissionsPolicyStore,
		Name:         storeID,
		Tags:         cloneStringMap(store.Tags),
		Attributes: map[string]any{
			"policy_store_id":     storeID,
			"description":         strings.TrimSpace(store.Description),
			"validation_mode":     strings.TrimSpace(store.ValidationMode),
			"deletion_protection": strings.TrimSpace(store.DeletionProtection),
			"encryption_state":    strings.TrimSpace(store.EncryptionState),
			"cedar_version":       strings.TrimSpace(store.CedarVersion),
			"created_date":        timeOrNil(store.CreatedDate),
			"last_updated_date":   timeOrNil(store.LastUpdatedDate),
		},
		CorrelationAnchors: []string{storeARN, storeID},
		SourceRecordID:     resourceID,
	}
}

func policyObservation(boundary aws.Boundary, policy Policy) aws.ResourceObservation {
	policyID := strings.TrimSpace(policy.ID)
	resourceID := policyResourceID(policy)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeVerifiedPermissionsPolicy,
		Name:         policyID,
		Attributes: map[string]any{
			"policy_id":         policyID,
			"policy_store_id":   strings.TrimSpace(policy.PolicyStoreID),
			"policy_type":       strings.TrimSpace(policy.PolicyType),
			"effect":            strings.TrimSpace(policy.Effect),
			"created_date":      timeOrNil(policy.CreatedDate),
			"last_updated_date": timeOrNil(policy.LastUpdatedDate),
		},
		CorrelationAnchors: []string{resourceID, policyID},
		SourceRecordID:     resourceID,
	}
}

func identitySourceObservation(boundary aws.Boundary, source IdentitySource) aws.ResourceObservation {
	sourceID := strings.TrimSpace(source.ID)
	resourceID := identitySourceResourceID(source)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeVerifiedPermissionsIdentitySource,
		Name:         sourceID,
		Attributes: map[string]any{
			"identity_source_id":    sourceID,
			"policy_store_id":       strings.TrimSpace(source.PolicyStoreID),
			"principal_entity_type": strings.TrimSpace(source.PrincipalEntityType),
			"provider_kind":         strings.TrimSpace(source.ProviderKind),
			"cognito_user_pool_arn": strings.TrimSpace(source.CognitoUserPoolARN),
			"openid_issuer":         strings.TrimSpace(source.OpenIDIssuer),
			"client_id_count":       source.ClientIDCount,
			"created_date":          timeOrNil(source.CreatedDate),
			"last_updated_date":     timeOrNil(source.LastUpdatedDate),
		},
		CorrelationAnchors: []string{resourceID, sourceID},
		SourceRecordID:     resourceID,
	}
}
