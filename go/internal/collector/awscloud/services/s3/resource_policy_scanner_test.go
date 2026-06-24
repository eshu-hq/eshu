// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsResourcePolicyPermissionFacts(t *testing.T) {
	client := fakeClient{buckets: []Bucket{{
		Name: "orders-artifacts",
		ResourcePolicyStatements: []ResourcePolicyStatement{
			{
				StatementSID:        "AllowPartner",
				Effect:              "Allow",
				Actions:             []string{"s3:GetObject"},
				Resources:           []string{"arn:aws:s3:::orders-artifacts/*"},
				PrincipalAccountIDs: []string{"999988887777"},
				PrincipalARNs:       []string{"arn:aws:iam::999988887777:role/partner"},
				PrincipalTypes:      []string{awscloud.ResourcePolicyPrincipalTypeAWS},
				IsCrossAccount:      true,
			},
			{
				StatementSID:   "DenyInsecure",
				Effect:         "Deny",
				Actions:        []string{"s3:*"},
				Resources:      []string{"arn:aws:s3:::orders-artifacts/*"},
				ConditionKeys:  []string{"aws:SecureTransport"},
				PrincipalTypes: []string{awscloud.ResourcePolicyPrincipalTypeAWS},
				IsPublic:       true,
			},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// One fact per statement.
	if got := countResourcePolicyPermissions(envelopes); got != 2 {
		t.Fatalf("aws_resource_policy_permission count = %d, want 2", got)
	}

	allow := resourcePolicyPermissionByEffect(t, envelopes, "Allow")
	assertPayloadEquals(t, allow.Payload, "resource_arn", "arn:aws:s3:::orders-artifacts")
	assertPayloadEquals(t, allow.Payload, "resource_type", awscloud.ResourceTypeS3Bucket)
	assertPayloadEquals(t, allow.Payload, "policy_source", awscloud.ResourcePolicySourceResource)
	assertPayloadEquals(t, allow.Payload, "is_cross_account", true)
	if got, _ := allow.Payload["principal_account_ids"].([]string); len(got) != 1 || got[0] != "999988887777" {
		t.Fatalf("allow principal_account_ids = %#v, want [999988887777]", allow.Payload["principal_account_ids"])
	}

	deny := resourcePolicyPermissionByEffect(t, envelopes, "Deny")
	assertPayloadEquals(t, deny.Payload, "is_public", true)
	assertPayloadEquals(t, deny.Payload, "has_conditions", true)

	// Forbidden: no raw policy JSON, statement Sid/body, or condition values.
	for _, envelope := range []facts.Envelope{allow, deny} {
		for _, forbidden := range []string{
			"policy", "policy_json", "policy_document", "statement", "statement_sid",
			"sid", "condition", "conditions", "condition_values",
		} {
			if _, exists := envelope.Payload[forbidden]; exists {
				t.Fatalf("resource policy permission fact carries forbidden key %q", forbidden)
			}
		}
	}
}

func TestScannerEmitsNoResourcePolicyPermissionFactWithoutPolicy(t *testing.T) {
	client := fakeClient{buckets: []Bucket{{Name: "no-policy-bucket"}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countResourcePolicyPermissions(envelopes); got != 0 {
		t.Fatalf("aws_resource_policy_permission count = %d, want 0 for a bucket with no policy", got)
	}
}

func countResourcePolicyPermissions(envelopes []facts.Envelope) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSResourcePolicyPermissionFactKind {
			count++
		}
	}
	return count
}

func resourcePolicyPermissionByEffect(t *testing.T, envelopes []facts.Envelope, effect string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourcePolicyPermissionFactKind {
			continue
		}
		if got, _ := envelope.Payload["effect"].(string); got == effect {
			return envelope
		}
	}
	t.Fatalf("missing aws_resource_policy_permission with effect %q in %#v", effect, envelopes)
	return facts.Envelope{}
}

func assertPayloadEquals(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()
	if got := payload[key]; got != want {
		t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
	}
}
