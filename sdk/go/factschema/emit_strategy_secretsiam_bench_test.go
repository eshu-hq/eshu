// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"testing"

	secretsiamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/secretsiam/v1"
)

func BenchmarkSecretsIAMEncodeDirectMap(b *testing.B) {
	policy := benchmarkSecretsIAMPermissionPolicy()
	b.Run("aws_permission_policy_json_roundtrip", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			payload, err := encodeToPayload(policy)
			if err != nil {
				b.Fatalf("encodeToPayload() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})
	b.Run("aws_permission_policy_direct_map", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeAWSIAMPermissionPolicy(policy)
			if err != nil {
				b.Fatalf("EncodeAWSIAMPermissionPolicy() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})
}

func benchmarkSecretsIAMPermissionPolicy() secretsiamv1.AWSIAMPermissionPolicy {
	return secretsiamv1.AWSIAMPermissionPolicy{
		AccountID:              "123456789012",
		Region:                 "us-east-1",
		Provider:               "aws_iam",
		CollectorInstanceID:    "collector-1",
		RedactionPolicyVersion: "secrets-iam-v1",
		PrincipalARN:           "arn:aws:iam::123456789012:role/eshu-runtime",
		PolicySource:           "inline",
		Effect:                 "Allow",
		PrincipalType:          stringPtr("role"),
		PolicyARN:              stringPtr("arn:aws:iam::123456789012:policy/eshu-runtime"),
		PolicyName:             stringPtr("eshu-runtime-inline"),
		StatementSID:           stringPtr("AllowSecretRead"),
		Actions:                []string{"secretsmanager:getsecretvalue", "kms:decrypt"},
		NotActions:             []string{"iam:*"},
		Resources:              []string{"arn:aws:secretsmanager:us-east-1:123456789012:secret:*"},
		NotResources:           []string{"arn:aws:s3:::raw"},
		ConditionKeys:          []string{"aws:SourceVpce", "aws:PrincipalOrgID"},
		ConditionOperators:     []string{"StringEquals", "ForAnyValue:StringLike"},
		ConditionOperatorCount: intPtr(2),
		HasConditions:          boolPtr(true),
		IsWildcardAction:       boolPtr(false),
		IsWildcardResource:     boolPtr(false),
	}
}

func intPtr(value int) *int { return &value }
