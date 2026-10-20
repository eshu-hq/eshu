// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Direct Connect metadata facts for one claimed account and
// region. It never reads or persists the BGP authentication key (authKey) that
// virtual interfaces carry, nor MACsec connectivity association key names or
// secret ARNs that connections and LAGs carry; AWS-reported identity,
// ownership, state, and bounded option metadata are the product.
//
// Direct Connect is a hybrid-networking family. Virtual interfaces attach to
// Direct Connect gateways and physical connections; Direct Connect gateways
// associate with transit gateways or virtual private gateways owned by the
// transitgateway and vpc scanners. The Direct Connect gateway resource this
// scanner emits uses resource_type aws_direct_connect_gateway and the bare
// gateway ID as resource_id, which matches the edge the transitgateway scanner
// already emits, so that previously dangling edge resolves once this runs.
type Scanner struct {
	Client Client
}

// Scan observes Direct Connect metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("direct connect scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDirectConnect:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDirectConnect
	default:
		return nil, fmt.Errorf("direct connect scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	connections, err := s.Client.ListConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list direct connect connections: %w", err)
	}
	for _, connection := range connections {
		emitted, err := connectionEnvelopes(boundary, connection)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	lags, err := s.Client.ListLAGs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list direct connect lags: %w", err)
	}
	for _, lag := range lags {
		resource, err := awscloud.NewResourceEnvelope(lagObservation(boundary, lag))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	gateways, err := s.Client.ListGateways(ctx)
	if err != nil {
		return nil, fmt.Errorf("list direct connect gateways: %w", err)
	}
	for _, gateway := range gateways {
		resource, err := awscloud.NewResourceEnvelope(gatewayObservation(boundary, gateway))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	virtualInterfaces, err := s.Client.ListVirtualInterfaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list direct connect virtual interfaces: %w", err)
	}
	for _, vif := range virtualInterfaces {
		emitted, err := virtualInterfaceEnvelopes(boundary, vif)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, emitted...)
	}

	associations, err := s.Client.ListGatewayAssociations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list direct connect gateway associations: %w", err)
	}
	for _, association := range associations {
		observation, ok := gatewayAssociationRelationship(boundary, association)
		if !ok {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func connectionEnvelopes(boundary awscloud.Boundary, connection Connection) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(connectionObservation(boundary, connection))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range connectionRelationships(boundary, connection) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func virtualInterfaceEnvelopes(boundary awscloud.Boundary, vif VirtualInterface) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(virtualInterfaceObservation(boundary, vif))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range virtualInterfaceRelationships(boundary, vif) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}
