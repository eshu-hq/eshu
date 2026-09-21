// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resourcegroups

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceResourceGroups,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:resourcegroups:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        17,
		ObservedAt:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	}
}

// fakeClient is a static Resource Groups client for tests. It satisfies the
// metadata-only Client interface without reaching AWS.
type fakeClient struct {
	groups []Group
	err    error
}

func (c fakeClient) ListGroups(context.Context) ([]Group, error) {
	return c.groups, c.err
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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func relationships(envelopes []facts.Envelope) []awscloud.RelationshipObservation {
	var out []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		out = append(out, observationFromPayload(envelope.Payload))
	}
	return out
}

// observationFromPayload reconstructs the relationship contract fields the
// relguard runtime helper checks from an emitted relationship payload, so the
// scanner test enforces the graph-join contract on the data the scanner
// actually produced.
func observationFromPayload(payload map[string]any) awscloud.RelationshipObservation {
	str := func(key string) string {
		value, _ := payload[key].(string)
		return value
	}
	return awscloud.RelationshipObservation{
		RelationshipType: str("relationship_type"),
		SourceResourceID: str("source_resource_id"),
		SourceARN:        str("source_arn"),
		TargetResourceID: str("target_resource_id"),
		TargetARN:        str("target_arn"),
		TargetType:       str("target_type"),
	}
}

func relationshipTo(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType, targetResourceID string,
) awscloud.RelationshipObservation {
	t.Helper()
	for _, obs := range relationships(envelopes) {
		if obs.RelationshipType == relationshipType && obs.TargetResourceID == targetResourceID {
			return obs
		}
	}
	t.Fatalf("missing relationship %q -> %q", relationshipType, targetResourceID)
	return awscloud.RelationshipObservation{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
