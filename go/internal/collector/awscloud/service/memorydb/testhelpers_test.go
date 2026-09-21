// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package memorydb

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
		ServiceKind:         awscloud.ServiceMemoryDB,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:memorydb:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 18, 30, 0, 0, time.UTC),
	}
}

// fakeClient returns canned MemoryDB metadata so scanner tests exercise fact
// and relationship emission without the AWS SDK adapter.
type fakeClient struct {
	clusters        []Cluster
	subnetGroups    []SubnetGroup
	parameterGroups []ParameterGroup
	users           []User
	acls            []ACL
	snapshots       []SnapshotMetadata
}

func (c fakeClient) ListClusters(context.Context) ([]Cluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListSubnetGroups(context.Context) ([]SubnetGroup, error) {
	return c.subnetGroups, nil
}

func (c fakeClient) ListParameterGroups(context.Context) ([]ParameterGroup, error) {
	return c.parameterGroups, nil
}

func (c fakeClient) ListUsers(context.Context) ([]User, error) {
	return c.users, nil
}

func (c fakeClient) ListACLs(context.Context) ([]ACL, error) {
	return c.acls, nil
}

func (c fakeClient) ListSnapshots(context.Context) ([]SnapshotMetadata, error) {
	return c.snapshots, nil
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
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
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
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func assertRelationshipTarget(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	targetID string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == targetID {
			return
		}
		if got, _ := envelope.Payload["target_arn"].(string); got == targetID {
			return
		}
	}
	t.Fatalf("missing relationship %q target %q in %#v", relationshipType, targetID, envelopes)
}

func assertRelationshipSourceRecordID(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	want string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if envelope.SourceRef.SourceRecordID == want {
			return
		}
	}
	t.Fatalf("relationship %q SourceRecordID %q not found", relationshipType, want)
}

func assertRelationshipTargetAttribute(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	key string,
	want string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		attrs, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			continue
		}
		if got, _ := attrs[key].(string); got == want {
			return
		}
	}
	t.Fatalf("missing relationship %q with attribute %s=%q", relationshipType, key, want)
}

func countRelationships(envelopes []facts.Envelope) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotStrings, ok := got.([]string)
		if !ok || len(gotStrings) != len(want) {
			return false
		}
		for i := range want {
			if gotStrings[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
