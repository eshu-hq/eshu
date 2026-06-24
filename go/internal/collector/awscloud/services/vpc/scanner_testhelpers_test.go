// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpc

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func scanFixture(t *testing.T, client fakeClient) []facts.Envelope {
	t.Helper()
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	return envelopes
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceVPC,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:vpc:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	routeTables      []RouteTable
	routeTablesErr   error
	internetGateways []InternetGateway
	natGateways      []NATGateway
	networkACLs      []NetworkACL
	peerings         []VPCPeeringConnection
	endpoints        []VPCEndpoint
	elasticIPs       []ElasticIP
	dhcpOptions      []DHCPOptions
	customerGateways []CustomerGateway
	vpnGateways      []VPNGateway
	vpnConnections   []VPNConnection
}

func (c fakeClient) ListRouteTables(context.Context) ([]RouteTable, error) {
	return c.routeTables, c.routeTablesErr
}

func (c fakeClient) ListInternetGateways(context.Context) ([]InternetGateway, error) {
	return c.internetGateways, nil
}

func (c fakeClient) ListNATGateways(context.Context) ([]NATGateway, error) {
	return c.natGateways, nil
}

func (c fakeClient) ListNetworkACLs(context.Context) ([]NetworkACL, error) {
	return c.networkACLs, nil
}

func (c fakeClient) ListVPCPeeringConnections(context.Context) ([]VPCPeeringConnection, error) {
	return c.peerings, nil
}

func (c fakeClient) ListVPCEndpoints(context.Context) ([]VPCEndpoint, error) {
	return c.endpoints, nil
}

func (c fakeClient) ListElasticIPs(context.Context) ([]ElasticIP, error) {
	return c.elasticIPs, nil
}

func (c fakeClient) ListDHCPOptions(context.Context) ([]DHCPOptions, error) {
	return c.dhcpOptions, nil
}

func (c fakeClient) ListCustomerGateways(context.Context) ([]CustomerGateway, error) {
	return c.customerGateways, nil
}

func (c fakeClient) ListVPNGateways(context.Context) ([]VPNGateway, error) {
	return c.vpnGateways, nil
}

func (c fakeClient) ListVPNConnections(context.Context) ([]VPNConnection, error) {
	return c.vpnConnections, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceType, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got != resourceType {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q with resource_id %q", resourceType, resourceID)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
