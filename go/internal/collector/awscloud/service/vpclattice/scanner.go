// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpclattice

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon VPC Lattice metadata-only facts for one claimed account
// and region. It reports service networks, services, target groups, and
// listeners plus the service-network-to-VPC, service-network-to-service,
// listener-in-service, target-group-to-VPC, target-group-to-service,
// target-group-to-target (Lambda, EC2 instance, ALB), and
// service-to-ACM-certificate relationships. It never reads or persists
// auth-policy bodies, resource-policy bodies, or any data-plane payload, and
// never mutates VPC Lattice state.
type Scanner struct {
	// Client is the metadata-only VPC Lattice snapshot source.
	Client Client
}

// Scan observes VPC Lattice service networks, services, target groups, and
// listeners plus the direct association and dependency edges through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("vpclattice scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceVPCLattice:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceVPCLattice
	default:
		return nil, fmt.Errorf("vpclattice scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot VPC Lattice metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, network := range snapshot.ServiceNetworks {
		next, err := serviceNetworkEnvelopes(boundary, network)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, service := range snapshot.Services {
		next, err := serviceEnvelopes(boundary, service)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, group := range snapshot.TargetGroups {
		next, err := targetGroupEnvelopes(boundary, group)
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

func serviceNetworkEnvelopes(boundary awscloud.Boundary, network ServiceNetwork) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serviceNetworkObservation(boundary, network))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, association := range network.VPCAssociations {
		rel := serviceNetworkVPCRelationship(boundary, network, association)
		if err := appendRelationship(&envelopes, rel); err != nil {
			return nil, err
		}
	}
	for _, association := range network.ServiceAssociations {
		rel := serviceNetworkServiceRelationship(boundary, network, association)
		if err := appendRelationship(&envelopes, rel); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func serviceEnvelopes(boundary awscloud.Boundary, service Service) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serviceObservation(boundary, service))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationship(&envelopes, serviceCertificateRelationship(boundary, service)); err != nil {
		return nil, err
	}
	for _, listener := range service.Listeners {
		listenerResource, err := awscloud.NewResourceEnvelope(listenerObservation(boundary, service, listener))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, listenerResource)
		if err := appendRelationship(&envelopes, listenerInServiceRelationship(boundary, service, listener)); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func targetGroupEnvelopes(boundary awscloud.Boundary, group TargetGroup) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(targetGroupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if err := appendRelationship(&envelopes, targetGroupVPCRelationship(boundary, group)); err != nil {
		return nil, err
	}
	for _, serviceARN := range group.ServiceARNs {
		if err := appendRelationship(&envelopes, targetGroupServiceRelationship(boundary, group, serviceARN)); err != nil {
			return nil, err
		}
	}
	for _, target := range group.Targets {
		if err := appendRelationship(&envelopes, targetGroupTargetRelationship(boundary, group, target)); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func appendRelationship(envelopes *[]facts.Envelope, relationship *awscloud.RelationshipObservation) error {
	if relationship == nil {
		return nil
	}
	envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func serviceNetworkObservation(boundary awscloud.Boundary, network ServiceNetwork) awscloud.ResourceObservation {
	arn := strings.TrimSpace(network.ARN)
	name := strings.TrimSpace(network.Name)
	resourceID := serviceNetworkResourceID(network)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeVPCLatticeServiceNetwork,
		Name:         name,
		Tags:         cloneStringMap(network.Tags),
		Attributes: map[string]any{
			"service_network_id":                           strings.TrimSpace(network.ID),
			"number_of_associated_services":                network.NumberOfAssociatedServices,
			"number_of_associated_vpcs":                    network.NumberOfAssociatedVPCs,
			"number_of_associated_resource_configurations": network.NumberOfAssociatedResourceConfigurations,
			"created_at":                                   timeOrNil(network.CreatedAt),
			"last_updated_at":                              timeOrNil(network.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func serviceObservation(boundary awscloud.Boundary, service Service) awscloud.ResourceObservation {
	arn := strings.TrimSpace(service.ARN)
	name := strings.TrimSpace(service.Name)
	resourceID := serviceResourceID(service)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeVPCLatticeService,
		Name:         name,
		State:        strings.TrimSpace(service.Status),
		Tags:         cloneStringMap(service.Tags),
		Attributes: map[string]any{
			"service_id":            strings.TrimSpace(service.ID),
			"custom_domain_name":    strings.TrimSpace(service.CustomDomainName),
			"dns_entry_domain_name": strings.TrimSpace(service.DNSEntryDomainName),
			"auth_type":             strings.TrimSpace(service.AuthType),
			"certificate_arn":       strings.TrimSpace(service.CertificateARN),
			"created_at":            timeOrNil(service.CreatedAt),
			"last_updated_at":       timeOrNil(service.LastUpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func listenerObservation(boundary awscloud.Boundary, service Service, listener Listener) awscloud.ResourceObservation {
	arn := strings.TrimSpace(listener.ARN)
	name := strings.TrimSpace(listener.Name)
	resourceID := listenerResourceID(listener)
	attributes := map[string]any{
		"listener_id":  strings.TrimSpace(listener.ID),
		"protocol":     strings.TrimSpace(listener.Protocol),
		"service_arn":  strings.TrimSpace(service.ARN),
		"service_id":   strings.TrimSpace(service.ID),
		"service_name": strings.TrimSpace(service.Name),
	}
	if listener.Port != 0 {
		attributes["port"] = listener.Port
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeVPCLatticeListener,
		Name:               name,
		Tags:               nil,
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func targetGroupObservation(boundary awscloud.Boundary, group TargetGroup) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := targetGroupResourceID(group)
	attributes := map[string]any{
		"target_group_id": strings.TrimSpace(group.ID),
		"type":            strings.TrimSpace(group.Type),
		"protocol":        strings.TrimSpace(group.Protocol),
		"ip_address_type": strings.TrimSpace(group.IPAddressType),
		"vpc_id":          strings.TrimSpace(group.VPCID),
		"service_arns":    cloneStrings(group.ServiceARNs),
		"target_count":    int64(len(group.Targets)),
		"created_at":      timeOrNil(group.CreatedAt),
		"last_updated_at": timeOrNil(group.LastUpdatedAt),
	}
	if group.Port != 0 {
		attributes["port"] = group.Port
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeVPCLatticeTargetGroup,
		Name:               name,
		State:              strings.TrimSpace(group.Status),
		Tags:               cloneStringMap(group.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
