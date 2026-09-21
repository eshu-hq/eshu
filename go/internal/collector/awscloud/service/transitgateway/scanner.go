// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Transit Gateway metadata facts for one claimed account and
// region. It never reads transit gateway routes, multicast group memberships,
// or policy table rules; AWS-reported identity, ownership, state, and bounded
// option metadata are the product.
//
// Cross-account peering attachments are emitted with the remote transit
// gateway identity exactly as AWS reports it. The scanner never resolves the
// remote account's identity; downstream org-context joins own that work.
type Scanner struct {
	Client Client
}

// Scan observes transit gateway metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("transit gateway scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceTransitGateway:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceTransitGateway
	default:
		return nil, fmt.Errorf("transit gateway scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	gateways, err := s.Client.ListTransitGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transit gateways: %w", err)
	}
	for _, gateway := range gateways {
		resource, err := awscloud.NewResourceEnvelope(transitGatewayObservation(boundary, gateway))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	routeTables, err := s.Client.ListTransitGatewayRouteTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transit gateway route tables: %w", err)
	}
	for _, routeTable := range routeTables {
		emitted, err := routeTableEnvelopes(boundary, routeTable)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	attachments, err := s.Client.ListTransitGatewayAttachments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transit gateway attachments: %w", err)
	}
	for _, attachment := range attachments {
		emitted, err := attachmentEnvelopes(boundary, attachment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	peerings, err := s.Client.ListTransitGatewayPeeringAttachments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transit gateway peering attachments: %w", err)
	}
	for _, peering := range peerings {
		emitted, err := peeringAttachmentEnvelopes(boundary, peering)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	multicastDomains, err := s.Client.ListTransitGatewayMulticastDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transit gateway multicast domains: %w", err)
	}
	for _, domain := range multicastDomains {
		emitted, err := multicastDomainEnvelopes(boundary, domain)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	policyTables, err := s.Client.ListTransitGatewayPolicyTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("list transit gateway policy tables: %w", err)
	}
	for _, policyTable := range policyTables {
		emitted, err := policyTableEnvelopes(boundary, policyTable)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	return envelopes, nil
}

func routeTableEnvelopes(boundary awscloud.Boundary, rt RouteTable) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(routeTableObservation(boundary, rt))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range routeTableRelationships(boundary, rt) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func attachmentEnvelopes(boundary awscloud.Boundary, attachment Attachment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(attachmentObservation(boundary, attachment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range attachmentRelationships(boundary, attachment) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func peeringAttachmentEnvelopes(boundary awscloud.Boundary, peering PeeringAttachment) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(peeringAttachmentObservation(boundary, peering))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range peeringAttachmentRelationships(boundary, peering) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func multicastDomainEnvelopes(boundary awscloud.Boundary, domain MulticastDomain) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(multicastDomainObservation(boundary, domain))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range multicastDomainRelationships(boundary, domain) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func policyTableEnvelopes(boundary awscloud.Boundary, policyTable PolicyTable) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(policyTableObservation(boundary, policyTable))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range policyTableRelationships(boundary, policyTable) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
