// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

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
		ServiceKind:         awscloud.ServiceDirectConnect,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:directconnect:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	connections         []Connection
	connectionsErr      error
	virtualInterfaces   []VirtualInterface
	gateways            []Gateway
	lags                []LAG
	gatewayAssociations []GatewayAssociation
}

func (c fakeClient) ListConnections(context.Context) ([]Connection, error) {
	return c.connections, c.connectionsErr
}

func (c fakeClient) ListVirtualInterfaces(context.Context) ([]VirtualInterface, error) {
	return c.virtualInterfaces, nil
}

func (c fakeClient) ListGateways(context.Context) ([]Gateway, error) {
	return c.gateways, nil
}

func (c fakeClient) ListLAGs(context.Context) ([]LAG, error) {
	return c.lags, nil
}

func (c fakeClient) ListGatewayAssociations(context.Context) ([]GatewayAssociation, error) {
	return c.gatewayAssociations, nil
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

func hasRelationship(envelopes []facts.Envelope, relationshipType string) bool {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return true
		}
	}
	return false
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
