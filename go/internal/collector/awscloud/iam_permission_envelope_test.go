// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewIAMPermissionEnvelopeNormalizesStatement(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	envelope, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:      boundary,
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: ResourceTypeIAMRole,
		PolicySource:  IAMPolicySourceInline,
		PolicyName:    "inline-escalate",
		StatementSID:  "AllowPassRole",
		Effect:        "Allow",
		Actions:       []string{"iam:passrole", "iam:PassRole", "  sts:AssumeRole  "},
		Resources:     []string{"arn:aws:iam::123456789012:role/*"},
		ConditionKeys: []string{"aws:SourceIp", "aws:SourceIp"},
		ConditionOperators: []string{
			"StringEquals",
			" IpAddress ",
			"StringEquals",
		},
	})
	if err != nil {
		t.Fatalf("NewIAMPermissionEnvelope returned error: %v", err)
	}
	if envelope.FactKind != facts.AWSIAMPermissionFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSIAMPermissionFactKind)
	}
	if envelope.SchemaVersion != facts.AWSIAMPermissionSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.AWSIAMPermissionSchemaVersion)
	}
	assertPayloadString(t, envelope.Payload, "principal_arn", "arn:aws:iam::123456789012:role/eshu-runtime")
	assertPayloadString(t, envelope.Payload, "principal_type", ResourceTypeIAMRole)
	assertPayloadString(t, envelope.Payload, "policy_source", IAMPolicySourceInline)
	assertPayloadString(t, envelope.Payload, "effect", "Allow")

	actions, ok := envelope.Payload["actions"].([]string)
	if !ok {
		t.Fatalf("actions = %T, want []string", envelope.Payload["actions"])
	}
	// Normalization: trimmed, lowercased, de-duplicated, sorted. The two
	// iam:PassRole spellings collapse to one entry.
	want := []string{"iam:passrole", "sts:assumerole"}
	if len(actions) != len(want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("actions[%d] = %q, want %q", i, actions[i], want[i])
		}
	}

	if got, _ := envelope.Payload["has_conditions"].(bool); !got {
		t.Fatalf("has_conditions = %v, want true", got)
	}
	keys, ok := envelope.Payload["condition_keys"].([]string)
	if !ok {
		t.Fatalf("condition_keys = %T, want []string", envelope.Payload["condition_keys"])
	}
	if len(keys) != 1 || keys[0] != "aws:SourceIp" {
		t.Fatalf("condition_keys = %v, want [aws:SourceIp] (de-duplicated, no values)", keys)
	}
	operators, ok := envelope.Payload["condition_operators"].([]string)
	if !ok {
		t.Fatalf("condition_operators = %T, want []string", envelope.Payload["condition_operators"])
	}
	if len(operators) != 2 || operators[0] != "IpAddress" || operators[1] != "StringEquals" {
		t.Fatalf("condition_operators = %v, want [IpAddress StringEquals]", operators)
	}
	if got, _ := envelope.Payload["condition_operator_count"].(int); got != 2 {
		t.Fatalf("condition_operator_count = %v, want 2", got)
	}
}

func TestNewIAMPermissionEnvelopeWildcardActionAndResource(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	envelope, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:      boundary,
		PrincipalARN:  "arn:aws:iam::123456789012:user/admin",
		PrincipalType: ResourceTypeIAMUser,
		PolicySource:  IAMPolicySourceAttachedManaged,
		PolicyARN:     "arn:aws:iam::aws:policy/AdministratorAccess",
		Effect:        "Allow",
		Actions:       []string{"*"},
		Resources:     []string{"*"},
	})
	if err != nil {
		t.Fatalf("NewIAMPermissionEnvelope returned error: %v", err)
	}
	if got, _ := envelope.Payload["is_wildcard_action"].(bool); !got {
		t.Fatalf("is_wildcard_action = %v, want true", got)
	}
	if got, _ := envelope.Payload["is_wildcard_resource"].(bool); !got {
		t.Fatalf("is_wildcard_resource = %v, want true", got)
	}
	if got, _ := envelope.Payload["has_conditions"].(bool); got {
		t.Fatalf("has_conditions = %v, want false", got)
	}
	assertPayloadString(t, envelope.Payload, "policy_arn", "arn:aws:iam::aws:policy/AdministratorAccess")
}

func TestNewIAMPermissionEnvelopeTrustStatementCapturesAssumePrincipals(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	envelope, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:         boundary,
		PrincipalARN:     "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType:    ResourceTypeIAMRole,
		PolicySource:     IAMPolicySourceTrust,
		Effect:           "Allow",
		Actions:          []string{"sts:AssumeRole"},
		AssumePrincipals: []string{"arn:aws:iam::111122223333:root", "arn:aws:iam::111122223333:root"},
	})
	if err != nil {
		t.Fatalf("NewIAMPermissionEnvelope returned error: %v", err)
	}
	assertPayloadString(t, envelope.Payload, "policy_source", IAMPolicySourceTrust)
	principals, ok := envelope.Payload["assume_principals"].([]string)
	if !ok {
		t.Fatalf("assume_principals = %T, want []string", envelope.Payload["assume_principals"])
	}
	if len(principals) != 1 || principals[0] != "arn:aws:iam::111122223333:root" {
		t.Fatalf("assume_principals = %v, want one de-duplicated entry", principals)
	}
}

func TestNewIAMPermissionEnvelopeRequiresPrincipalAndEffect(t *testing.T) {
	boundary := testBoundary(time.Now())
	if _, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:     boundary,
		PolicySource: IAMPolicySourceInline,
		Effect:       "Allow",
		Actions:      []string{"s3:GetObject"},
	}); err == nil {
		t.Fatal("NewIAMPermissionEnvelope() error = nil, want missing principal error")
	}
	if _, err := NewIAMPermissionEnvelope(IAMPermissionObservation{
		Boundary:     boundary,
		PrincipalARN: "arn:aws:iam::123456789012:role/app",
		PolicySource: IAMPolicySourceInline,
		Actions:      []string{"s3:GetObject"},
	}); err == nil {
		t.Fatal("NewIAMPermissionEnvelope() error = nil, want missing effect error")
	}
}

func TestNewIAMPermissionEnvelopeStableIdentityIgnoresConditionOrder(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	base := IAMPermissionObservation{
		Boundary:      boundary,
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: ResourceTypeIAMRole,
		PolicySource:  IAMPolicySourceInline,
		PolicyName:    "inline-escalate",
		StatementSID:  "AllowPassRole",
		Effect:        "Allow",
		Actions:       []string{"iam:PassRole"},
		Resources:     []string{"arn:aws:iam::123456789012:role/*"},
		ConditionKeys: []string{"aws:SourceIp", "aws:PrincipalOrgID"},
		ConditionOperators: []string{
			"IpAddress",
			"StringEquals",
		},
	}
	first, err := NewIAMPermissionEnvelope(base)
	if err != nil {
		t.Fatalf("first envelope error: %v", err)
	}
	reordered := base
	reordered.Actions = []string{"IAM:PassRole"}
	reordered.ConditionKeys = []string{"aws:PrincipalOrgID", "aws:SourceIp", "aws:SourceIp"}
	reordered.ConditionOperators = []string{"StringEquals", "IpAddress", "StringEquals"}
	second, err := NewIAMPermissionEnvelope(reordered)
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

func TestNewIAMPermissionEnvelopeStableIdentityIncludesConditionSummary(t *testing.T) {
	boundary := testBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	base := IAMPermissionObservation{
		Boundary:      boundary,
		PrincipalARN:  "arn:aws:iam::123456789012:role/eshu-runtime",
		PrincipalType: ResourceTypeIAMRole,
		PolicySource:  IAMPolicySourceInline,
		PolicyName:    "inline-escalate",
		Effect:        "Allow",
		Actions:       []string{"iam:PassRole"},
		Resources:     []string{"arn:aws:iam::123456789012:role/*"},
	}
	unconditional, err := NewIAMPermissionEnvelope(base)
	if err != nil {
		t.Fatalf("unconditional envelope error: %v", err)
	}
	wantUnconditionalKey := facts.StableID(facts.AWSIAMPermissionFactKind, map[string]any{
		"account_id":    boundary.AccountID,
		"actions":       "iam:passrole",
		"effect":        "Allow",
		"not_actions":   "",
		"not_resources": "",
		"policy_arn":    "",
		"policy_name":   "inline-escalate",
		"policy_source": IAMPolicySourceInline,
		"principal_arn": "arn:aws:iam::123456789012:role/eshu-runtime",
		"region":        boundary.Region,
		"resources":     "arn:aws:iam::123456789012:role/*",
		"statement_sid": "",
	})
	if unconditional.StableFactKey != wantUnconditionalKey {
		t.Fatalf("unconditional StableFactKey = %q, want legacy key %q", unconditional.StableFactKey, wantUnconditionalKey)
	}

	sourceIPCondition := base
	sourceIPCondition.ConditionKeys = []string{"aws:SourceIp"}
	sourceIPCondition.ConditionOperators = []string{"IpAddress"}
	sourceIP, err := NewIAMPermissionEnvelope(sourceIPCondition)
	if err != nil {
		t.Fatalf("source-ip envelope error: %v", err)
	}
	stringCondition := base
	stringCondition.ConditionKeys = []string{"aws:SourceIp"}
	stringCondition.ConditionOperators = []string{"StringEquals"}
	stringEquals, err := NewIAMPermissionEnvelope(stringCondition)
	if err != nil {
		t.Fatalf("string-equals envelope error: %v", err)
	}
	orgCondition := base
	orgCondition.ConditionKeys = []string{"aws:PrincipalOrgID"}
	orgCondition.ConditionOperators = []string{"IpAddress"}
	org, err := NewIAMPermissionEnvelope(orgCondition)
	if err != nil {
		t.Fatalf("org envelope error: %v", err)
	}
	assertDistinctFactIdentity(t, sourceIP, stringEquals, org)
}
