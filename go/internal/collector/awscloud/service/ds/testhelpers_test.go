// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ds

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
		ServiceKind:         awscloud.ServiceDirectoryService,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ds:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

// fakeClient is a static Directory Service read surface for scanner tests. The
// per-directory maps key trusts, shares, and LDAPS settings by directory id so
// tests exercise the scanner's per-directory fan-out without an AWS dependency.
type fakeClient struct {
	directories []Directory
	trusts      map[string][]Trust
	shares      map[string][]SharedDirectory
	ldaps       map[string][]LDAPSSetting
}

func (c fakeClient) ListDirectories(context.Context) ([]Directory, error) {
	return c.directories, nil
}

func (c fakeClient) ListTrusts(_ context.Context, directoryID string) ([]Trust, error) {
	return c.trusts[directoryID], nil
}

func (c fakeClient) ListSharedDirectories(_ context.Context, ownerDirectoryID string) ([]SharedDirectory, error) {
	return c.shares[ownerDirectoryID], nil
}

func (c fakeClient) ListLDAPSSettings(_ context.Context, directoryID string) ([]LDAPSSetting, error) {
	return c.ldaps[directoryID], nil
}

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_id %q", resourceID)
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
	if countRelationships(envelopes, relationshipType) == 0 {
		t.Fatalf("missing relationship_type %q", relationshipType)
	}
}

func assertRelationshipJoinKeys(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relType, _ := envelope.Payload["relationship_type"].(string)
		if got, _ := envelope.Payload["target_type"].(string); got == "" {
			t.Fatalf("relationship %q has empty target_type", relType)
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == "" {
			t.Fatalf("relationship %q has empty target_resource_id", relType)
		}
	}
}

func countResources(envelopes []facts.Envelope, resourceType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			count++
		}
	}
	return count
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
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
