// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon OpenSearch Serverless metadata-only facts for one claimed
// account and region. It never reads the OpenSearch HTTP data plane (index,
// search, bulk, document APIs), never persists access-policy or security-policy
// document bodies, and never mutates Serverless state. It reports collections,
// security policies, and managed VPC endpoints plus the collection-to-KMS-key
// (from the matching encryption policy) and VPC-endpoint-to-VPC/subnet/
// security-group relationships.
type Scanner struct {
	// Client is the metadata-only OpenSearch Serverless snapshot source.
	Client Client
}

// Scan observes OpenSearch Serverless collections, security policies, managed VPC
// endpoints, and their direct KMS and EC2 dependency metadata through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("opensearchserverless scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceOpenSearchServerless:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceOpenSearchServerless
	default:
		return nil, fmt.Errorf("opensearchserverless scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot OpenSearch Serverless metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, collection := range snapshot.Collections {
		next, err := collectionEnvelopes(boundary, collection, snapshot.EncryptionKeyBindings)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, policy := range snapshot.SecurityPolicies {
		envelope, err := securityPolicyEnvelope(boundary, policy)
		if err != nil {
			return nil, err
		}
		if envelope != nil {
			envelopes = append(envelopes, *envelope)
		}
	}
	for _, endpoint := range snapshot.VPCEndpoints {
		next, err := vpcEndpointEnvelopes(boundary, endpoint)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func collectionEnvelopes(
	boundary awscloud.Boundary,
	collection Collection,
	bindings []EncryptionKeyBinding,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(collectionObservation(boundary, collection))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := collectionKMSRelationship(boundary, collection, bindings); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func vpcEndpointEnvelopes(boundary awscloud.Boundary, endpoint VPCEndpoint) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(vpcEndpointObservation(boundary, endpoint))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range vpcEndpointRelationships(boundary, endpoint) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func collectionObservation(boundary awscloud.Boundary, collection Collection) awscloud.ResourceObservation {
	collectionARN := strings.TrimSpace(collection.ARN)
	name := strings.TrimSpace(collection.Name)
	resourceID := collectionResourceID(collection)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          collectionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeOpenSearchServerlessAOSSCollection,
		Name:         name,
		State:        strings.TrimSpace(collection.Status),
		Tags:         cloneStringMap(collection.Tags),
		Attributes: map[string]any{
			"collection_id":      strings.TrimSpace(collection.ID),
			"collection_type":    strings.TrimSpace(collection.Type),
			"standby_replicas":   strings.TrimSpace(collection.StandbyReplicas),
			"kms_key_arn":        strings.TrimSpace(collection.KMSKeyARN),
			"created_date":       timeOrNil(collection.CreatedDate),
			"last_modified_date": timeOrNil(collection.LastModifiedDate),
		},
		CorrelationAnchors: []string{collectionARN, name},
		SourceRecordID:     resourceID,
	}
}

func securityPolicyEnvelope(boundary awscloud.Boundary, policy SecurityPolicy) (*facts.Envelope, error) {
	resourceID := securityPolicyResourceID(policy)
	if resourceID == "" {
		return nil, nil
	}
	envelope, err := awscloud.NewResourceEnvelope(awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeOpenSearchServerlessSecurityPolicy,
		Name:         strings.TrimSpace(policy.Name),
		Attributes: map[string]any{
			"policy_type":        strings.TrimSpace(policy.Type),
			"policy_version":     strings.TrimSpace(policy.PolicyVersion),
			"description":        strings.TrimSpace(policy.Description),
			"created_date":       timeOrNil(policy.CreatedDate),
			"last_modified_date": timeOrNil(policy.LastModifiedDate),
		},
		CorrelationAnchors: []string{strings.TrimSpace(policy.Name)},
		SourceRecordID:     resourceID,
	})
	if err != nil {
		return nil, err
	}
	return &envelope, nil
}

func vpcEndpointObservation(boundary awscloud.Boundary, endpoint VPCEndpoint) awscloud.ResourceObservation {
	name := strings.TrimSpace(endpoint.Name)
	resourceID := vpcEndpointResourceID(endpoint)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeOpenSearchServerlessAOSSVPCEndpoint,
		Name:         name,
		State:        strings.TrimSpace(endpoint.Status),
		Attributes: map[string]any{
			"endpoint_id":        strings.TrimSpace(endpoint.ID),
			"vpc_id":             strings.TrimSpace(endpoint.VPCID),
			"subnet_ids":         cloneStrings(endpoint.SubnetIDs),
			"security_group_ids": cloneStrings(endpoint.SecurityGroupIDs),
			"created_date":       timeOrNil(endpoint.CreatedDate),
		},
		CorrelationAnchors: []string{strings.TrimSpace(endpoint.ID), name},
		SourceRecordID:     resourceID,
	}
}
