// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

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
		ServiceKind:         awscloud.ServiceNeptune,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:neptune:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	clusters        []DBCluster
	instances       []ClusterInstance
	parameterGroups []ClusterParameterGroup
	snapshots       []ClusterSnapshot
	subnetGroups    []SubnetGroup
	globalClusters  []GlobalCluster
	graphs          []Graph
	graphSnapshots  []GraphSnapshot
}

func (c fakeClient) ListDBClusters(context.Context) ([]DBCluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListClusterInstances(context.Context) ([]ClusterInstance, error) {
	return c.instances, nil
}

func (c fakeClient) ListClusterParameterGroups(context.Context) ([]ClusterParameterGroup, error) {
	return c.parameterGroups, nil
}

func (c fakeClient) ListClusterSnapshots(context.Context) ([]ClusterSnapshot, error) {
	return c.snapshots, nil
}

func (c fakeClient) ListSubnetGroups(context.Context) ([]SubnetGroup, error) {
	return c.subnetGroups, nil
}

func (c fakeClient) ListGlobalClusters(context.Context) ([]GlobalCluster, error) {
	return c.globalClusters, nil
}

func (c fakeClient) ListGraphs(context.Context) ([]Graph, error) {
	return c.graphs, nil
}

func (c fakeClient) ListGraphSnapshots(context.Context) ([]GraphSnapshot, error) {
	return c.graphSnapshots, nil
}

// forbiddenSubstrings lists attribute-key and correlation-anchor fragments that
// must never appear in emitted Neptune facts. Neptune is RDS/DocumentDB-shaped,
// so master-credential leakage is the primary risk; graph data-plane leakage
// (vertex/edge contents, query results) is the Neptune Analytics risk.
func forbiddenSubstrings() []string {
	return []string{
		"master_username",
		"masterusername",
		"password",
		"secret",
		"vertex",
		"edge",
		"query_result",
		"query_results",
	}
}

func contains(value, substr string) bool {
	return len(substr) > 0 && len(value) >= len(substr) && indexOf(value, substr) >= 0
}

func indexOf(value, substr string) int {
	for i := 0; i+len(substr) <= len(value); i++ {
		if value[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func assertForbiddenAbsent(t *testing.T, attributes map[string]any, _ string) {
	t.Helper()
	for key := range attributes {
		for _, forbidden := range forbiddenSubstrings() {
			if contains(key, forbidden) {
				t.Fatalf("attribute %q contains forbidden substring %q; Neptune scanner must stay metadata-only", key, forbidden)
			}
		}
	}
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

func assertRelationshipTarget(t *testing.T, envelopes []facts.Envelope, relationshipType, targetID string) {
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
	t.Fatalf("missing relationship %q target %q", relationshipType, targetID)
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

func valuesEqual(got, want any) bool {
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
