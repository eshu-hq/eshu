// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kms

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsResourcePolicyPermissionFacts(t *testing.T) {
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
	client := fakeClient{keys: []Key{{
		ID:  "abcd-1234",
		ARN: keyARN,
		ResourcePolicyStatements: []ResourcePolicyStatement{
			{
				StatementSID:        "AllowPartnerDecrypt",
				Effect:              "Allow",
				Actions:             []string{"kms:Decrypt"},
				Resources:           []string{"*"},
				PrincipalAccountIDs: []string{"999988887777"},
				PrincipalARNs:       []string{"arn:aws:iam::999988887777:role/partner"},
				PrincipalTypes:      []string{awscloud.ResourcePolicyPrincipalTypeAWS},
				IsCrossAccount:      true,
			},
			{
				StatementSID:   "AllowRoot",
				Effect:         "Allow",
				Actions:        []string{"kms:*"},
				Resources:      []string{"*"},
				PrincipalTypes: []string{awscloud.ResourcePolicyPrincipalTypeAWS},
			},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	if got := countResourcePolicyPermissions(envelopes); got != 2 {
		t.Fatalf("aws_resource_policy_permission count = %d, want 2", got)
	}

	cross := resourcePolicyPermissionBySID(t, envelopes, "999988887777")
	assertKMSPayloadEquals(t, cross.Payload, "resource_arn", keyARN)
	assertKMSPayloadEquals(t, cross.Payload, "resource_type", awscloud.ResourceTypeKMSKey)
	assertKMSPayloadEquals(t, cross.Payload, "policy_source", awscloud.ResourcePolicySourceResource)
	assertKMSPayloadEquals(t, cross.Payload, "is_cross_account", true)
	// kms:* is a service-wildcard, not the bare "*" action wildcard, so the
	// is_wildcard_action flag stays false (matching aws_iam_permission's exact
	// behavior); only Resource "*" sets is_wildcard_resource.
	assertKMSPayloadEquals(t, cross.Payload, "is_wildcard_action", false)
	assertKMSPayloadEquals(t, cross.Payload, "is_wildcard_resource", true)

	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourcePolicyPermissionFactKind {
			continue
		}
		for _, forbidden := range []string{
			"policy", "policy_json", "policy_document", "statement", "statement_sid",
			"sid", "condition", "conditions", "condition_values", "key_material",
		} {
			if _, exists := envelope.Payload[forbidden]; exists {
				t.Fatalf("kms resource policy permission fact carries forbidden key %q", forbidden)
			}
		}
	}
}

func TestScannerEmitsNoResourcePolicyPermissionFactWithoutPolicy(t *testing.T) {
	client := fakeClient{keys: []Key{{ID: "no-policy", ARN: "arn:aws:kms:us-east-1:123456789012:key/no-policy"}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countResourcePolicyPermissions(envelopes); got != 0 {
		t.Fatalf("aws_resource_policy_permission count = %d, want 0 for a key with no policy", got)
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

func resourcePolicyPermissionBySID(t *testing.T, envelopes []facts.Envelope, accountID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourcePolicyPermissionFactKind {
			continue
		}
		accounts, _ := envelope.Payload["principal_account_ids"].([]string)
		for _, account := range accounts {
			if account == accountID {
				return envelope
			}
		}
	}
	t.Fatalf("missing aws_resource_policy_permission for account %q in %#v", accountID, envelopes)
	return facts.Envelope{}
}

func assertKMSPayloadEquals(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()
	if got := payload[key]; got != want {
		t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
	}
}
