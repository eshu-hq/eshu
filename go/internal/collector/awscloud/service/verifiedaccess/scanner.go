// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedaccess

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Verified Access metadata-only facts for one claimed
// account and region. It never reads or persists trust-provider client secrets,
// policy bodies, or any data-plane payload, and never mutates Verified Access
// state. It reports instances, groups, endpoints, and trust providers plus the
// group-in-instance, endpoint-in-group, instance-uses-trust-provider,
// endpoint-to-subnet, endpoint-to-security-group, and endpoint-to-ACM-certificate
// relationships.
type Scanner struct {
	// Client is the metadata-only Verified Access snapshot source.
	Client Client
}

// Scan observes Verified Access instances, groups, endpoints, and trust
// providers plus their direct subnet, security-group, and ACM-certificate
// dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("verifiedaccess scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceVerifiedAccess:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceVerifiedAccess
	default:
		return nil, fmt.Errorf("verifiedaccess scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Verified Access resources: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, instance := range snapshot.Instances {
		next, err := instanceEnvelopes(boundary, instance)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, trustProvider := range snapshot.TrustProviders {
		envelope, err := awscloud.NewResourceEnvelope(trustProviderObservation(boundary, trustProvider))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, group := range snapshot.Groups {
		next, err := groupEnvelopes(boundary, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, endpoint := range snapshot.Endpoints {
		next, err := endpointEnvelopes(boundary, endpoint)
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

func instanceEnvelopes(boundary awscloud.Boundary, instance Instance) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(instanceObservation(boundary, instance))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	next, err := relationshipEnvelopes(instanceTrustProviderRelationships(boundary, instance))
	if err != nil {
		return nil, err
	}
	return append(envelopes, next...), nil
}

func groupEnvelopes(boundary awscloud.Boundary, group Group) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(groupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := groupInInstanceRelationship(boundary, group); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func endpointEnvelopes(boundary awscloud.Boundary, endpoint Endpoint) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(endpointObservation(boundary, endpoint))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	var relationships []awscloud.RelationshipObservation
	if relationship := endpointInGroupRelationship(boundary, endpoint); relationship != nil {
		relationships = append(relationships, *relationship)
	}
	relationships = append(relationships, endpointSubnetRelationships(boundary, endpoint)...)
	relationships = append(relationships, endpointSecurityGroupRelationships(boundary, endpoint)...)
	if relationship := endpointACMCertificateRelationship(boundary, endpoint); relationship != nil {
		relationships = append(relationships, *relationship)
	}
	next, err := relationshipEnvelopes(relationships)
	if err != nil {
		return nil, err
	}
	return append(envelopes, next...), nil
}

func relationshipEnvelopes(observations []awscloud.RelationshipObservation) ([]facts.Envelope, error) {
	var envelopes []facts.Envelope
	for _, observation := range observations {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// endpointResourceID returns the resource_id the endpoint node publishes: the
// synthesized partition-aware endpoint ARN, falling back to the bare endpoint
// id, so the endpoint's own edges are sourced on the same id the node publishes.
func endpointResourceID(boundary awscloud.Boundary, endpoint Endpoint) string {
	if arn := resourceARN(boundary, "verified-access-endpoint", endpoint.ID); arn != "" && hasIdentity(boundary) {
		return arn
	}
	return strings.TrimSpace(endpoint.ID)
}

func instanceObservation(boundary awscloud.Boundary, instance Instance) awscloud.ResourceObservation {
	resourceID := instanceResourceID(boundary, instance.ID)
	bareID := strings.TrimSpace(instance.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeVerifiedAccessInstance,
		Name:         bareID,
		Tags:         cloneStringMap(instance.Tags),
		Attributes: map[string]any{
			"instance_id":                  bareID,
			"description":                  strings.TrimSpace(instance.Description),
			"fips_enabled":                 instance.FIPSEnabled,
			"customer_managed_key_enabled": instance.CustomerManagedKeyEnabled,
			"trust_provider_ids":           cloneStrings(instance.TrustProviderIDs),
			"creation_time":                timeOrNil(instance.CreationTime),
			"last_updated_time":            timeOrNil(instance.LastUpdatedTime),
		},
		CorrelationAnchors: correlationAnchors(resourceID, bareID),
		SourceRecordID:     resourceID,
	}
}

func groupObservation(boundary awscloud.Boundary, group Group) awscloud.ResourceObservation {
	resourceID := groupResourceID(boundary, group)
	bareID := strings.TrimSpace(group.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeVerifiedAccessGroup,
		Name:         bareID,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"group_id":                     bareID,
			"instance_id":                  strings.TrimSpace(group.InstanceID),
			"owner":                        strings.TrimSpace(group.Owner),
			"description":                  strings.TrimSpace(group.Description),
			"customer_managed_key_enabled": group.CustomerManagedKeyEnabled,
			"creation_time":                timeOrNil(group.CreationTime),
			"last_updated_time":            timeOrNil(group.LastUpdatedTime),
		},
		CorrelationAnchors: correlationAnchors(resourceID, bareID),
		SourceRecordID:     resourceID,
	}
}

func endpointObservation(boundary awscloud.Boundary, endpoint Endpoint) awscloud.ResourceObservation {
	resourceID := endpointResourceID(boundary, endpoint)
	bareID := strings.TrimSpace(endpoint.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeVerifiedAccessEndpoint,
		Name:         bareID,
		State:        strings.TrimSpace(endpoint.Status),
		Tags:         cloneStringMap(endpoint.Tags),
		Attributes: map[string]any{
			"endpoint_id":            bareID,
			"group_id":               strings.TrimSpace(endpoint.GroupID),
			"instance_id":            strings.TrimSpace(endpoint.InstanceID),
			"description":            strings.TrimSpace(endpoint.Description),
			"endpoint_type":          strings.TrimSpace(endpoint.EndpointType),
			"attachment_type":        strings.TrimSpace(endpoint.AttachmentType),
			"application_domain":     strings.TrimSpace(endpoint.ApplicationDomain),
			"endpoint_domain":        strings.TrimSpace(endpoint.EndpointDomain),
			"domain_certificate_arn": strings.TrimSpace(endpoint.DomainCertificateARN),
			"subnet_ids":             cloneStrings(endpoint.SubnetIDs),
			"security_group_ids":     cloneStrings(endpoint.SecurityGroupIDs),
			"load_balancer_arn":      strings.TrimSpace(endpoint.LoadBalancerARN),
			"network_interface_id":   strings.TrimSpace(endpoint.NetworkInterfaceID),
			"creation_time":          timeOrNil(endpoint.CreationTime),
			"last_updated_time":      timeOrNil(endpoint.LastUpdatedTime),
		},
		CorrelationAnchors: correlationAnchors(resourceID, bareID),
		SourceRecordID:     resourceID,
	}
}

func trustProviderObservation(boundary awscloud.Boundary, trustProvider TrustProvider) awscloud.ResourceObservation {
	resourceID := trustProviderResourceID(boundary, trustProvider.ID)
	bareID := strings.TrimSpace(trustProvider.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arnOrEmpty(resourceID),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeVerifiedAccessTrustProvider,
		Name:         bareID,
		Tags:         cloneStringMap(trustProvider.Tags),
		Attributes: map[string]any{
			"trust_provider_id":            bareID,
			"description":                  strings.TrimSpace(trustProvider.Description),
			"trust_provider_type":          strings.TrimSpace(trustProvider.TrustProviderType),
			"user_trust_provider_type":     strings.TrimSpace(trustProvider.UserTrustProviderType),
			"device_trust_provider_type":   strings.TrimSpace(trustProvider.DeviceTrustProviderType),
			"policy_reference_name":        strings.TrimSpace(trustProvider.PolicyReferenceName),
			"oidc_issuer":                  strings.TrimSpace(trustProvider.OIDCIssuer),
			"uses_iam_identity_center":     usesIAMIdentityCenter(trustProvider),
			"customer_managed_key_enabled": trustProvider.CustomerManagedKeyEnabled,
			"creation_time":                timeOrNil(trustProvider.CreationTime),
			"last_updated_time":            timeOrNil(trustProvider.LastUpdatedTime),
		},
		CorrelationAnchors: correlationAnchors(resourceID, bareID),
		SourceRecordID:     resourceID,
	}
}

// usesIAMIdentityCenter reports whether the trust provider delegates user
// identity to AWS IAM Identity Center. AWS does not expose the backing IAM
// Identity Center instance ARN on the trust provider, so the dependency is
// recorded as a metadata attribute rather than a dangling graph edge.
func usesIAMIdentityCenter(trustProvider TrustProvider) bool {
	return strings.EqualFold(strings.TrimSpace(trustProvider.UserTrustProviderType), "iam-identity-center")
}

// correlationAnchors returns the trimmed, de-duplicated, non-empty anchors for a
// resource node, preferring the resource_id then the bare id.
func correlationAnchors(values ...string) []string {
	anchors := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		anchors = append(anchors, trimmed)
	}
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}
