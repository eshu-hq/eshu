// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func resourcePolicyBoundary(observedAt time.Time) Boundary {
	boundary := testBoundary(observedAt)
	boundary.Region = "us-east-1"
	boundary.ServiceKind = ServiceS3
	boundary.ScopeID = "aws:123456789012:us-east-1"
	boundary.GenerationID = "aws:123456789012:us-east-1:s3:1"
	return boundary
}

func TestNewResourcePolicyPermissionEnvelopeCrossAccountAllow(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	envelope, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:            boundary,
		ResourceARN:         "arn:aws:s3:::eshu-shared-bucket",
		ResourceType:        ResourceTypeS3Bucket,
		StatementSID:        "AllowPartner",
		Effect:              "Allow",
		Actions:             []string{"s3:GetObject", "s3:getobject", "  S3:ListBucket  "},
		Resources:           []string{"arn:aws:s3:::eshu-shared-bucket/*"},
		PrincipalARNs:       []string{"arn:aws:iam::111122223333:role/partner"},
		PrincipalAccountIDs: []string{"111122223333"},
		PrincipalTypes:      []string{ResourcePolicyPrincipalTypeAWS},
		IsCrossAccount:      true,
	})
	if err != nil {
		t.Fatalf("NewResourcePolicyPermissionEnvelope returned error: %v", err)
	}
	if envelope.FactKind != facts.AWSResourcePolicyPermissionFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSResourcePolicyPermissionFactKind)
	}
	if envelope.SchemaVersion != facts.AWSResourcePolicyPermissionSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.AWSResourcePolicyPermissionSchemaVersion)
	}
	assertPayloadString(t, envelope.Payload, "resource_arn", "arn:aws:s3:::eshu-shared-bucket")
	assertPayloadString(t, envelope.Payload, "resource_type", ResourceTypeS3Bucket)
	assertPayloadString(t, envelope.Payload, "account_id", "123456789012")
	assertPayloadString(t, envelope.Payload, "region", "us-east-1")
	assertPayloadString(t, envelope.Payload, "policy_source", ResourcePolicySourceResource)
	assertPayloadString(t, envelope.Payload, "effect", "Allow")

	actions := payloadStrings(t, envelope.Payload, "actions")
	// Normalization: trimmed, lowercased, de-duplicated, sorted. The two
	// s3:GetObject spellings collapse to one entry.
	wantActions := []string{"s3:getobject", "s3:listbucket"}
	assertStringSlice(t, "actions", actions, wantActions)

	accounts := payloadStrings(t, envelope.Payload, "principal_account_ids")
	assertStringSlice(t, "principal_account_ids", accounts, []string{"111122223333"})

	principalARNs := payloadStrings(t, envelope.Payload, "principal_arns")
	assertStringSlice(t, "principal_arns", principalARNs, []string{"arn:aws:iam::111122223333:role/partner"})

	principalTypes := payloadStrings(t, envelope.Payload, "principal_types")
	assertStringSlice(t, "principal_types", principalTypes, []string{ResourcePolicyPrincipalTypeAWS})

	assertPayloadBool(t, envelope.Payload, "is_cross_account", true)
	assertPayloadBool(t, envelope.Payload, "is_public", false)
	assertPayloadBool(t, envelope.Payload, "has_conditions", false)
	assertPayloadBool(t, envelope.Payload, "is_wildcard_action", false)
	assertPayloadBool(t, envelope.Payload, "is_wildcard_resource", false)
}

func TestNewResourcePolicyPermissionEnvelopePublicPrincipal(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	envelope, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:       boundary,
		ResourceARN:    "arn:aws:s3:::eshu-public-bucket",
		ResourceType:   ResourceTypeS3Bucket,
		Effect:         "Allow",
		Actions:        []string{"s3:GetObject"},
		Resources:      []string{"arn:aws:s3:::eshu-public-bucket/*"},
		PrincipalTypes: []string{ResourcePolicyPrincipalTypeAWS},
		IsPublic:       true,
	})
	if err != nil {
		t.Fatalf("NewResourcePolicyPermissionEnvelope returned error: %v", err)
	}
	assertPayloadBool(t, envelope.Payload, "is_public", true)
	assertPayloadBool(t, envelope.Payload, "is_cross_account", false)
	// A public grant names no account id, so the list stays typed but empty.
	accounts := payloadStrings(t, envelope.Payload, "principal_account_ids")
	if len(accounts) != 0 {
		t.Fatalf("principal_account_ids = %v, want empty for public principal", accounts)
	}
}

func TestNewResourcePolicyPermissionEnvelopeDenyStatement(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	envelope, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:     boundary,
		ResourceARN:  "arn:aws:kms:us-east-1:123456789012:key/abcd",
		ResourceType: ResourceTypeKMSKey,
		Effect:       "Deny",
		Actions:      []string{"kms:Decrypt"},
		Resources:    []string{"*"},
		IsPublic:     true,
	})
	if err != nil {
		t.Fatalf("NewResourcePolicyPermissionEnvelope returned error: %v", err)
	}
	assertPayloadString(t, envelope.Payload, "effect", "Deny")
	assertPayloadString(t, envelope.Payload, "resource_type", ResourceTypeKMSKey)
	assertPayloadBool(t, envelope.Payload, "is_wildcard_resource", true)
}

func TestNewResourcePolicyPermissionEnvelopeConditionKeysCarryNamesOnly(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	envelope, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:      boundary,
		ResourceARN:   "arn:aws:s3:::eshu-conditioned",
		ResourceType:  ResourceTypeS3Bucket,
		Effect:        "Allow",
		Actions:       []string{"s3:GetObject"},
		Resources:     []string{"arn:aws:s3:::eshu-conditioned/*"},
		ConditionKeys: []string{"aws:SourceIp", "aws:SourceIp", "  aws:SecureTransport  "},
		ConditionOperators: []string{
			"IpAddress",
			" Bool ",
			"IpAddress",
		},
		PrincipalTypes: []string{ResourcePolicyPrincipalTypeAWS},
	})
	if err != nil {
		t.Fatalf("NewResourcePolicyPermissionEnvelope returned error: %v", err)
	}
	assertPayloadBool(t, envelope.Payload, "has_conditions", true)
	keys := payloadStrings(t, envelope.Payload, "condition_keys")
	// De-duplicated, trimmed, sorted: names only, never values.
	assertStringSlice(t, "condition_keys", keys, []string{"aws:SecureTransport", "aws:SourceIp"})
	operators := payloadStrings(t, envelope.Payload, "condition_operators")
	assertStringSlice(t, "condition_operators", operators, []string{"Bool", "IpAddress"})
	if got, _ := envelope.Payload["condition_operator_count"].(int); got != 2 {
		t.Fatalf("condition_operator_count = %v, want 2", got)
	}
}

func TestNewResourcePolicyPermissionEnvelopeWildcardActionAndResource(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	envelope, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:     boundary,
		ResourceARN:  "arn:aws:s3:::eshu-wild",
		ResourceType: ResourceTypeS3Bucket,
		Effect:       "Allow",
		Actions:      []string{"*"},
		Resources:    []string{"*"},
		IsPublic:     true,
	})
	if err != nil {
		t.Fatalf("NewResourcePolicyPermissionEnvelope returned error: %v", err)
	}
	assertPayloadBool(t, envelope.Payload, "is_wildcard_action", true)
	assertPayloadBool(t, envelope.Payload, "is_wildcard_resource", true)
}

func TestNewResourcePolicyPermissionEnvelopeRequiresIdentityFields(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Now())
	if _, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:     boundary,
		ResourceType: ResourceTypeS3Bucket,
		Effect:       "Allow",
		Actions:      []string{"s3:GetObject"},
	}); err == nil {
		t.Fatal("NewResourcePolicyPermissionEnvelope() error = nil, want missing resource_arn error")
	}
	if _, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:    boundary,
		ResourceARN: "arn:aws:s3:::eshu-bucket",
		Effect:      "Allow",
		Actions:     []string{"s3:GetObject"},
	}); err == nil {
		t.Fatal("NewResourcePolicyPermissionEnvelope() error = nil, want missing resource_type error")
	}
	if _, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:     boundary,
		ResourceARN:  "arn:aws:s3:::eshu-bucket",
		ResourceType: ResourceTypeS3Bucket,
		Effect:       "Permit",
		Actions:      []string{"s3:GetObject"},
	}); err == nil {
		t.Fatal("NewResourcePolicyPermissionEnvelope() error = nil, want invalid effect error")
	}
}

func TestNewResourcePolicyPermissionEnvelopeStableIdentityIgnoresActionCasing(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	base := ResourcePolicyPermissionObservation{
		Boundary:     boundary,
		ResourceARN:  "arn:aws:s3:::eshu-shared-bucket",
		ResourceType: ResourceTypeS3Bucket,
		StatementSID: "AllowPartner",
		Effect:       "Allow",
		Actions:      []string{"s3:GetObject"},
		Resources:    []string{"arn:aws:s3:::eshu-shared-bucket/*"},
		ConditionKeys: []string{
			"aws:SourceIp",
			"aws:PrincipalOrgID",
		},
		ConditionOperators: []string{"IpAddress", "StringEquals"},
	}
	first, err := NewResourcePolicyPermissionEnvelope(base)
	if err != nil {
		t.Fatalf("first envelope error: %v", err)
	}
	reordered := base
	reordered.Actions = []string{"S3:GETOBJECT"}
	reordered.ConditionKeys = []string{"aws:PrincipalOrgID", "aws:SourceIp", "aws:SourceIp"}
	reordered.ConditionOperators = []string{"StringEquals", "IpAddress", "StringEquals"}
	second, err := NewResourcePolicyPermissionEnvelope(reordered)
	if err != nil {
		t.Fatalf("second envelope error: %v", err)
	}
	if first.FactID != second.FactID {
		t.Fatalf("FactID changed with action casing: %q != %q", first.FactID, second.FactID)
	}
	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey changed with action casing: %q != %q", first.StableFactKey, second.StableFactKey)
	}
}

func TestNewResourcePolicyPermissionEnvelopeStableIdentityIncludesConditionSummary(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	base := ResourcePolicyPermissionObservation{
		Boundary:            boundary,
		ResourceARN:         "arn:aws:s3:::eshu-shared-bucket",
		ResourceType:        ResourceTypeS3Bucket,
		Effect:              "Allow",
		Actions:             []string{"s3:GetObject"},
		Resources:           []string{"arn:aws:s3:::eshu-shared-bucket/*"},
		PrincipalARNs:       []string{"arn:aws:iam::111122223333:role/partner"},
		PrincipalAccountIDs: []string{"111122223333"},
		PrincipalTypes:      []string{ResourcePolicyPrincipalTypeAWS},
	}
	unconditional, err := NewResourcePolicyPermissionEnvelope(base)
	if err != nil {
		t.Fatalf("unconditional envelope error: %v", err)
	}
	wantUnconditionalKey := facts.StableID(facts.AWSResourcePolicyPermissionFactKind, map[string]any{
		"account_id":            boundary.AccountID,
		"actions":               "s3:getobject",
		"effect":                "Allow",
		"not_actions":           "",
		"not_resources":         "",
		"policy_source":         ResourcePolicySourceResource,
		"principal_account_ids": "111122223333",
		"principal_arns":        "arn:aws:iam::111122223333:role/partner",
		"region":                boundary.Region,
		"resource_arn":          "arn:aws:s3:::eshu-shared-bucket",
		"resource_type":         ResourceTypeS3Bucket,
		"resources":             "arn:aws:s3:::eshu-shared-bucket/*",
		"statement_sid":         "",
	})
	if unconditional.StableFactKey != wantUnconditionalKey {
		t.Fatalf("unconditional StableFactKey = %q, want legacy key %q", unconditional.StableFactKey, wantUnconditionalKey)
	}

	sourceIPCondition := base
	sourceIPCondition.ConditionKeys = []string{"aws:SourceIp"}
	sourceIPCondition.ConditionOperators = []string{"IpAddress"}
	sourceIP, err := NewResourcePolicyPermissionEnvelope(sourceIPCondition)
	if err != nil {
		t.Fatalf("source-ip envelope error: %v", err)
	}
	stringCondition := base
	stringCondition.ConditionKeys = []string{"aws:SourceIp"}
	stringCondition.ConditionOperators = []string{"StringEquals"}
	stringEquals, err := NewResourcePolicyPermissionEnvelope(stringCondition)
	if err != nil {
		t.Fatalf("string-equals envelope error: %v", err)
	}
	orgCondition := base
	orgCondition.ConditionKeys = []string{"aws:PrincipalOrgID"}
	orgCondition.ConditionOperators = []string{"IpAddress"}
	org, err := NewResourcePolicyPermissionEnvelope(orgCondition)
	if err != nil {
		t.Fatalf("org envelope error: %v", err)
	}
	assertDistinctFactIdentity(t, sourceIP, stringEquals, org)
}

// TestNewResourcePolicyPermissionEnvelopeNeverPersistsForbiddenFields proves the
// payload carries only the derived/normalized contract: no raw policy JSON,
// statement Sid/body, or condition values reach the persisted fact.
func TestNewResourcePolicyPermissionEnvelopeNeverPersistsForbiddenFields(t *testing.T) {
	boundary := resourcePolicyBoundary(time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	envelope, err := NewResourcePolicyPermissionEnvelope(ResourcePolicyPermissionObservation{
		Boundary:      boundary,
		ResourceARN:   "arn:aws:s3:::eshu-shared-bucket",
		ResourceType:  ResourceTypeS3Bucket,
		StatementSID:  "AllowPartner",
		Effect:        "Allow",
		Actions:       []string{"s3:GetObject"},
		Resources:     []string{"arn:aws:s3:::eshu-shared-bucket/*"},
		ConditionKeys: []string{"aws:SourceIp"},
	})
	if err != nil {
		t.Fatalf("NewResourcePolicyPermissionEnvelope returned error: %v", err)
	}
	forbidden := []string{"statement_sid", "sid", "statement", "policy", "policy_document", "condition", "conditions", "condition_values"}
	for _, key := range forbidden {
		if _, ok := envelope.Payload[key]; ok {
			t.Fatalf("payload carries forbidden key %q; resource-policy facts are derived/normalized only", key)
		}
	}
}

func payloadStrings(t *testing.T, payload map[string]any, key string) []string {
	t.Helper()
	got, ok := payload[key].([]string)
	if !ok {
		t.Fatalf("payload[%q] = %T, want []string", key, payload[key])
	}
	return got
}

func assertStringSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q", name, i, got[i], want[i])
		}
	}
}

func assertDistinctFactIdentity(t *testing.T, envelopes ...facts.Envelope) {
	t.Helper()
	for left := 0; left < len(envelopes); left++ {
		for right := left + 1; right < len(envelopes); right++ {
			if envelopes[left].StableFactKey == envelopes[right].StableFactKey {
				t.Fatalf("StableFactKey[%d] = StableFactKey[%d] = %q", left, right, envelopes[left].StableFactKey)
			}
			if envelopes[left].FactID == envelopes[right].FactID {
				t.Fatalf("FactID[%d] = FactID[%d] = %q", left, right, envelopes[left].FactID)
			}
		}
	}
}
