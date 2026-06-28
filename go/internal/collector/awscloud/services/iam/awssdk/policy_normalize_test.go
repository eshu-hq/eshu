// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"slices"
	"testing"

	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

func TestNormalizePolicyDocumentExtractsStatementsWithoutRawJSON(t *testing.T) {
	raw := `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Sid": "AllowPassRole",
				"Effect": "Allow",
				"Action": ["iam:PassRole", "iam:passrole"],
				"Resource": "arn:aws:iam::123456789012:role/*",
				"Condition": {"StringEquals": {"aws:RequestedRegion": "us-east-1"}}
			},
			{
				"Effect": "Deny",
				"NotAction": "s3:*",
				"NotResource": ["arn:aws:s3:::secret/*"]
			}
		]
	}`
	statements, err := normalizePolicyDocument(raw, iamservice.PolicySourceInline, "", "inline-escalate")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v", err)
	}
	if len(statements) != 2 {
		t.Fatalf("len(statements) = %d, want 2", len(statements))
	}

	first := statements[0]
	if first.Source != iamservice.PolicySourceInline {
		t.Fatalf("Source = %q, want %q", first.Source, iamservice.PolicySourceInline)
	}
	if first.PolicyName != "inline-escalate" {
		t.Fatalf("PolicyName = %q, want inline-escalate", first.PolicyName)
	}
	if first.StatementSID != "AllowPassRole" {
		t.Fatalf("StatementSID = %q, want AllowPassRole", first.StatementSID)
	}
	if first.Effect != "Allow" {
		t.Fatalf("Effect = %q, want Allow", first.Effect)
	}
	if len(first.Actions) == 0 {
		t.Fatalf("Actions empty, want at least one action")
	}
	if got := first.ConditionKeys; len(got) != 1 || got[0] != "aws:RequestedRegion" {
		t.Fatalf("ConditionKeys = %v, want [aws:RequestedRegion] (key only, no value)", got)
	}
	if got := first.ConditionOperators; len(got) != 1 || got[0] != "StringEquals" {
		t.Fatalf("ConditionOperators = %v, want [StringEquals] (operator only, no value)", got)
	}

	second := statements[1]
	if second.Effect != "Deny" {
		t.Fatalf("Effect = %q, want Deny", second.Effect)
	}
	if len(second.NotActions) != 1 || second.NotActions[0] != "s3:*" {
		t.Fatalf("NotActions = %v, want [s3:*]", second.NotActions)
	}
	if len(second.NotResources) != 1 || second.NotResources[0] != "arn:aws:s3:::secret/*" {
		t.Fatalf("NotResources = %v, want [arn:aws:s3:::secret/*]", second.NotResources)
	}
}

func TestNormalizePolicyDocumentSummarizesConditionOperatorsWithoutValues(t *testing.T) {
	raw := `{
		"Statement": [{
			"Effect": "Allow",
			"Action": "s3:GetObject",
			"Resource": "arn:aws:s3:::prod-data/*",
			"Condition": {
				"StringEquals": {
					"aws:PrincipalTag/team": "payments",
					"aws:RequestedRegion": ["us-east-1", "us-west-2"]
				},
				"ForAnyValue:StringLike": {
					"aws:ResourceTag/env": "prod-*"
				},
				"ArnLike": {
					"aws:SourceArn": "arn:aws:lambda:us-east-1:123456789012:function:secret"
				}
			}
		}]
	}`
	statements, err := normalizePolicyDocument(raw, iamservice.PolicySourceInline, "", "inline-conditioned")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}

	gotKeys := statements[0].ConditionKeys
	wantKeys := []string{"aws:PrincipalTag/team", "aws:RequestedRegion", "aws:ResourceTag/env", "aws:SourceArn"}
	if !slices.Equal(gotKeys, wantKeys) {
		t.Fatalf("ConditionKeys = %v, want %v", gotKeys, wantKeys)
	}
	gotOperators := statements[0].ConditionOperators
	wantOperators := []string{"ArnLike", "ForAnyValue:StringLike", "StringEquals"}
	if !slices.Equal(gotOperators, wantOperators) {
		t.Fatalf("ConditionOperators = %v, want %v", gotOperators, wantOperators)
	}
	for _, leaked := range []string{"payments", "us-east-1", "prod-*", "arn:aws:lambda"} {
		if slices.Contains(gotOperators, leaked) || slices.Contains(gotKeys, leaked) {
			t.Fatalf("condition summary leaked value %q: keys=%v operators=%v", leaked, gotKeys, gotOperators)
		}
	}
}

func TestNormalizePolicyDocumentHandlesURLEncodedAndSingleStatement(t *testing.T) {
	// IAM policy documents arrive URL-encoded from several IAM APIs.
	raw := "%7B%22Statement%22%3A%7B%22Effect%22%3A%22Allow%22%2C%22Action%22%3A%22s3%3AGetObject%22%2C%22Resource%22%3A%22%2A%22%7D%7D"
	statements, err := normalizePolicyDocument(raw, iamservice.PolicySourceAttachedManaged, "arn:aws:iam::aws:policy/ReadOnly", "")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].PolicyARN != "arn:aws:iam::aws:policy/ReadOnly" {
		t.Fatalf("PolicyARN = %q", statements[0].PolicyARN)
	}
	if len(statements[0].Resources) != 1 || statements[0].Resources[0] != "*" {
		t.Fatalf("Resources = %v, want [*]", statements[0].Resources)
	}
}

func TestNormalizeTrustPolicyStatementsCaptureAssumePrincipals(t *testing.T) {
	raw := `{
		"Version": "2012-10-17",
		"Statement": [{
			"Effect": "Allow",
			"Principal": {"AWS": "arn:aws:iam::111122223333:root", "Service": "lambda.amazonaws.com"},
			"Action": "sts:AssumeRole"
		}]
	}`
	statements, err := normalizeTrustPolicyDocument(raw)
	if err != nil {
		t.Fatalf("normalizeTrustPolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].Source != iamservice.PolicySourceTrust {
		t.Fatalf("Source = %q, want %q", statements[0].Source, iamservice.PolicySourceTrust)
	}
	want := map[string]bool{
		"arn:aws:iam::111122223333:root": true,
		"lambda.amazonaws.com":           true,
	}
	if len(statements[0].AssumePrincipals) != len(want) {
		t.Fatalf("AssumePrincipals = %v, want %d entries", statements[0].AssumePrincipals, len(want))
	}
	for _, principal := range statements[0].AssumePrincipals {
		if !want[principal] {
			t.Fatalf("unexpected assume principal %q", principal)
		}
	}
}

func TestNormalizeTrustPolicyCapturesWebIdentitySubjectFingerprintOnly(t *testing.T) {
	raw := `{
		"Statement": [{
			"Effect": "Allow",
			"Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/cluster"},
			"Action": "sts:AssumeRoleWithWebIdentity",
			"Condition": {
				"StringEquals": {
					"oidc.eks.us-east-1.amazonaws.com/id/cluster:sub": "system:serviceaccount:checkout:payments-api"
				}
			}
		}]
	}`
	statements, err := normalizeTrustPolicyDocument(raw)
	if err != nil {
		t.Fatalf("normalizeTrustPolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	want := secretsiam.WebIdentitySubjectFingerprint("system:serviceaccount:checkout:payments-api")
	if !slices.Contains(statements[0].WebIdentitySubjectFingerprints, want) {
		t.Fatalf("WebIdentitySubjectFingerprints = %v, want %q", statements[0].WebIdentitySubjectFingerprints, want)
	}
	for _, got := range statements[0].WebIdentitySubjectFingerprints {
		if got == "system:serviceaccount:checkout:payments-api" {
			t.Fatal("WebIdentitySubjectFingerprints leaked raw subject")
		}
	}
	if statements[0].WebIdentitySubjectWildcard {
		t.Fatal("WebIdentitySubjectWildcard = true, want false for exact subject")
	}
}

func TestNormalizeTrustPolicyMarksWildcardWebIdentitySubject(t *testing.T) {
	raw := `{
		"Statement": [{
			"Effect": "Allow",
			"Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/example"},
			"Action": "sts:AssumeRoleWithWebIdentity",
			"Condition": {
				"StringLike": {
					"example:sub": "system:serviceaccount:*"
				}
			}
		}]
	}`
	statements, err := normalizeTrustPolicyDocument(raw)
	if err != nil {
		t.Fatalf("normalizeTrustPolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if !statements[0].WebIdentitySubjectWildcard {
		t.Fatal("WebIdentitySubjectWildcard = false, want true")
	}
	if len(statements[0].WebIdentitySubjectFingerprints) != 0 {
		t.Fatalf("WebIdentitySubjectFingerprints = %v, want none for wildcard", statements[0].WebIdentitySubjectFingerprints)
	}
}

func TestNormalizeTrustPolicyDoesNotMarkExactNonKubernetesSubjectAsWildcard(t *testing.T) {
	raw := `{
		"Statement": [{
			"Effect": "Allow",
			"Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"},
			"Action": "sts:AssumeRoleWithWebIdentity",
			"Condition": {
				"StringEquals": {
					"token.actions.githubusercontent.com:sub": "repo:eshu-hq/eshu:ref:refs/heads/main"
				}
			}
		}]
	}`
	statements, err := normalizeTrustPolicyDocument(raw)
	if err != nil {
		t.Fatalf("normalizeTrustPolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].WebIdentitySubjectWildcard {
		t.Fatal("WebIdentitySubjectWildcard = true, want false for exact unsupported subject")
	}
	if len(statements[0].WebIdentitySubjectFingerprints) != 0 {
		t.Fatalf("WebIdentitySubjectFingerprints = %v, want none for unsupported subject", statements[0].WebIdentitySubjectFingerprints)
	}
}

func TestNormalizePolicyDocumentEmptyOrBlankReturnsNil(t *testing.T) {
	statements, err := normalizePolicyDocument("   ", iamservice.PolicySourceInline, "", "x")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v", err)
	}
	if statements != nil {
		t.Fatalf("statements = %v, want nil for blank document", statements)
	}
}

// TestNormalizePolicyDocumentSkipsStatementWithNoEffect proves that
// normalizeIdentityStatement drops a statement whose Effect is missing or
// unrecognised so only Allow/Deny entries reach the caller.
func TestNormalizePolicyDocumentSkipsStatementWithNoEffect(t *testing.T) {
	raw := `{"Statement":[
		{"Sid":"Good","Effect":"Allow","Action":"s3:GetObject","Resource":"*"},
		{"Sid":"Bad","Action":"s3:DeleteObject","Resource":"*"}
	]}`
	statements, err := normalizePolicyDocument(raw, iamservice.PolicySourceInline, "", "mixed")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1 (no-effect statement must be skipped)", len(statements))
	}
	if statements[0].StatementSID != "Good" {
		t.Fatalf("StatementSID = %q, want Good", statements[0].StatementSID)
	}
}

// TestNormalizePolicyDocumentSingleObjectStatementIsAccepted proves that when
// the Statement field is a JSON object (not an array), documentStatements still
// returns it as a single-element slice that normalizePolicyDocument processes.
func TestNormalizePolicyDocumentSingleObjectStatementIsAccepted(t *testing.T) {
	raw := `{"Statement":{"Effect":"Deny","Action":"iam:DeleteRole","Resource":"*"}}`
	statements, err := normalizePolicyDocument(raw, iamservice.PolicySourceAttachedManaged, "arn:aws:iam::aws:policy/ReadOnly", "")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v, want nil", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].Effect != "Deny" {
		t.Fatalf("Effect = %q, want Deny", statements[0].Effect)
	}
	if len(statements[0].Actions) != 1 || statements[0].Actions[0] != "iam:DeleteRole" {
		t.Fatalf("Actions = %v, want [iam:DeleteRole]", statements[0].Actions)
	}
}

// TestNormalizePolicyDocumentActionAsStringSetsActions proves the stringList
// single-string path: when Action is a bare string (not an array), it must be
// returned as a one-element slice rather than nil.
func TestNormalizePolicyDocumentActionAsStringSetsActions(t *testing.T) {
	raw := `{"Statement":[{"Effect":"Allow","Action":"ec2:DescribeInstances","Resource":"*"}]}`
	statements, err := normalizePolicyDocument(raw, iamservice.PolicySourceInline, "", "single-action")
	if err != nil {
		t.Fatalf("normalizePolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if len(statements[0].Actions) != 1 || statements[0].Actions[0] != "ec2:DescribeInstances" {
		t.Fatalf("Actions = %v, want [ec2:DescribeInstances] (single string, not array)", statements[0].Actions)
	}
	if len(statements[0].Resources) != 1 || statements[0].Resources[0] != "*" {
		t.Fatalf("Resources = %v, want [*]", statements[0].Resources)
	}
}

// TestNormalizeTrustPolicyDenyEffectIsIncluded proves that a trust policy
// statement with Effect:"Deny" is normalized and returned — normalizeEffect
// accepts both Allow and Deny, so callers can reason about Deny-trust grants.
func TestNormalizeTrustPolicyDenyEffectIsIncluded(t *testing.T) {
	raw := `{"Statement":[{"Effect":"Deny","Principal":{"AWS":"arn:aws:iam::111122223333:root"},"Action":"sts:AssumeRole"}]}`
	statements, err := normalizeTrustPolicyDocument(raw)
	if err != nil {
		t.Fatalf("normalizeTrustPolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].Effect != "Deny" {
		t.Fatalf("Effect = %q, want Deny", statements[0].Effect)
	}
	if len(statements[0].AssumePrincipals) != 1 || statements[0].AssumePrincipals[0] != "arn:aws:iam::111122223333:root" {
		t.Fatalf("AssumePrincipals = %v, want [arn:aws:iam::111122223333:root]", statements[0].AssumePrincipals)
	}
}

// TestNormalizeTrustPolicyArrayPrincipalFlattened proves that when a trust
// statement's Principal field is a bare JSON array (not a keyed object), all
// elements are flattened into AssumePrincipals via trustPrincipalEntries([]any).
func TestNormalizeTrustPolicyArrayPrincipalFlattened(t *testing.T) {
	raw := `{"Statement":[{"Effect":"Allow","Principal":["arn:aws:iam::111122223333:root","arn:aws:iam::444455556666:role/cicd"],"Action":"sts:AssumeRole"}]}`
	statements, err := normalizeTrustPolicyDocument(raw)
	if err != nil {
		t.Fatalf("normalizeTrustPolicyDocument() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	got := statements[0].AssumePrincipals
	if len(got) != 2 {
		t.Fatalf("AssumePrincipals count = %d, want 2: %v", len(got), got)
	}
	wantSet := map[string]bool{
		"arn:aws:iam::111122223333:root":      true,
		"arn:aws:iam::444455556666:role/cicd": true,
	}
	for _, p := range got {
		if !wantSet[p] {
			t.Fatalf("unexpected principal %q in AssumePrincipals", p)
		}
	}
}
