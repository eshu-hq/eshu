// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	s3service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3"
)

func TestDeriveBucketPolicyResourcePermissionStatements(t *testing.T) {
	document := `{"Version":"2012-10-17","Statement":[
		{"Sid":"AllowPartner","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::999988887777:role/partner"},"Action":["s3:GetObject","s3:getobject"],"Resource":"arn:aws:s3:::b/*","Condition":{"StringEquals":{"aws:SourceVpc":"vpc-123"}}},
		{"Sid":"PublicRead","Effect":"Allow","Principal":"*","Action":"s3:ListBucket","Resource":"arn:aws:s3:::b"},
		{"Sid":"DenyInsecure","Effect":"Deny","Principal":{"AWS":"*"},"Action":"s3:*","Resource":"arn:aws:s3:::b/*","Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`

	statements, err := deriveBucketPolicyResourcePermissionStatements(document, "123456789012")
	if err != nil {
		t.Fatalf("deriveBucketPolicyResourcePermissionStatements() error = %v, want nil", err)
	}
	if len(statements) != 3 {
		t.Fatalf("statement count = %d, want 3: %#v", len(statements), statements)
	}

	allow := statementBySID(t, statements, "AllowPartner")
	if allow.Effect != "Allow" {
		t.Fatalf("AllowPartner effect = %q, want Allow", allow.Effect)
	}
	if !equalStrings(allow.Actions, []string{"s3:GetObject", "s3:getobject"}) {
		t.Fatalf("AllowPartner actions = %#v, want raw [s3:GetObject s3:getobject] (envelope normalizes)", allow.Actions)
	}
	if !equalStrings(allow.Resources, []string{"arn:aws:s3:::b/*"}) {
		t.Fatalf("AllowPartner resources = %#v", allow.Resources)
	}
	if !equalStrings(allow.PrincipalAccountIDs, []string{"999988887777"}) {
		t.Fatalf("AllowPartner principal_account_ids = %#v, want [999988887777]", allow.PrincipalAccountIDs)
	}
	if !equalStrings(allow.PrincipalARNs, []string{"arn:aws:iam::999988887777:role/partner"}) {
		t.Fatalf("AllowPartner principal_arns = %#v", allow.PrincipalARNs)
	}
	if !equalStrings(allow.PrincipalTypes, []string{awscloud.ResourcePolicyPrincipalTypeAWS}) {
		t.Fatalf("AllowPartner principal_types = %#v, want [aws]", allow.PrincipalTypes)
	}
	// Condition KEY only, never the value "vpc-123".
	if !equalStrings(allow.ConditionKeys, []string{"aws:SourceVpc"}) {
		t.Fatalf("AllowPartner condition_keys = %#v, want [aws:SourceVpc] (names only)", allow.ConditionKeys)
	}
	if !equalStrings(allow.ConditionOperators, []string{"StringEquals"}) {
		t.Fatalf("AllowPartner condition_operators = %#v, want [StringEquals]", allow.ConditionOperators)
	}
	if !allow.IsCrossAccount {
		t.Fatalf("AllowPartner is_cross_account = false, want true")
	}
	if allow.IsPublic {
		t.Fatalf("AllowPartner is_public = true, want false")
	}

	public := statementBySID(t, statements, "PublicRead")
	if !public.IsPublic {
		t.Fatalf("PublicRead is_public = false, want true")
	}
	if len(public.PrincipalAccountIDs) != 0 {
		t.Fatalf("PublicRead principal_account_ids = %#v, want empty", public.PrincipalAccountIDs)
	}

	deny := statementBySID(t, statements, "DenyInsecure")
	if deny.Effect != "Deny" {
		t.Fatalf("DenyInsecure effect = %q, want Deny", deny.Effect)
	}
	// A Deny statement is still emitted as a fact (the reducer applies
	// deny-precedence); is_public reflects the named principal "*".
	if !deny.IsPublic {
		t.Fatalf("DenyInsecure is_public = false, want true for {\"AWS\":\"*\"}")
	}
	if !equalStrings(deny.ConditionKeys, []string{"aws:SecureTransport"}) {
		t.Fatalf("DenyInsecure condition_keys = %#v, want [aws:SecureTransport]", deny.ConditionKeys)
	}
	if !equalStrings(deny.ConditionOperators, []string{"Bool"}) {
		t.Fatalf("DenyInsecure condition_operators = %#v, want [Bool]", deny.ConditionOperators)
	}
}

func TestDeriveBucketPolicyResourcePermissionStatementsNoActionNoResource(t *testing.T) {
	document := `{"Statement":[
		{"Sid":"NotActionStmt","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::999988887777:root"},"NotAction":"s3:DeleteBucket","NotResource":"arn:aws:s3:::protected"}]}`
	statements, err := deriveBucketPolicyResourcePermissionStatements(document, "123456789012")
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(statements) != 1 {
		t.Fatalf("statement count = %d, want 1", len(statements))
	}
	stmt := statements[0]
	if !equalStrings(stmt.NotActions, []string{"s3:DeleteBucket"}) {
		t.Fatalf("NotActions = %#v, want [s3:DeleteBucket]", stmt.NotActions)
	}
	if !equalStrings(stmt.NotResources, []string{"arn:aws:s3:::protected"}) {
		t.Fatalf("NotResources = %#v, want [arn:aws:s3:::protected]", stmt.NotResources)
	}
}

func TestDeriveBucketPolicyResourcePermissionStatementsEmptyPolicy(t *testing.T) {
	statements, err := deriveBucketPolicyResourcePermissionStatements(`{"Statement":[]}`, "123456789012")
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(statements) != 0 {
		t.Fatalf("statement count = %d, want 0", len(statements))
	}
}

func TestDeriveBucketPolicyResourcePermissionStatementsRejectsMalformed(t *testing.T) {
	if _, err := deriveBucketPolicyResourcePermissionStatements("{not json", "123456789012"); err == nil {
		t.Fatalf("error = nil, want parse error for malformed document")
	}
}

// TestDerivePrincipalFactsCanonicalAndFederatedTypes proves that CanonicalUser
// and Federated principal-key entries contribute their type to PrincipalTypes
// (via principalTypeForKey) without contributing account-ids or ARNs, and that
// neither sets the public or cross-account booleans.
func TestDerivePrincipalFactsCanonicalAndFederatedTypes(t *testing.T) {
	document := `{"Statement":[
		{"Sid":"Canon","Effect":"Allow","Principal":{"CanonicalUser":"79a59df900b949e55d96a1e698fbacedfd6e09d98eacf8f8d5218e7cd47ef2be"},"Action":"s3:GetObject","Resource":"*"},
		{"Sid":"Fed","Effect":"Allow","Principal":{"Federated":"arn:aws:iam::123456789012:oidc-provider/example"},"Action":"s3:GetObject","Resource":"*"}
	]}`
	statements, err := deriveBucketPolicyResourcePermissionStatements(document, "123456789012")
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(statements) != 2 {
		t.Fatalf("statement count = %d, want 2", len(statements))
	}

	canon := statementBySID(t, statements, "Canon")
	if !equalStrings(canon.PrincipalTypes, []string{awscloud.ResourcePolicyPrincipalTypeCanonical}) {
		t.Fatalf("Canon PrincipalTypes = %#v, want [canonical]", canon.PrincipalTypes)
	}
	if len(canon.PrincipalARNs) != 0 || len(canon.PrincipalAccountIDs) != 0 {
		t.Fatalf("Canon must not emit ARNs/accountIDs for CanonicalUser: arns=%v accounts=%v", canon.PrincipalARNs, canon.PrincipalAccountIDs)
	}
	if canon.IsPublic || canon.IsCrossAccount {
		t.Fatalf("Canon: is_public=%v is_cross_account=%v, want both false", canon.IsPublic, canon.IsCrossAccount)
	}

	fed := statementBySID(t, statements, "Fed")
	if !equalStrings(fed.PrincipalTypes, []string{awscloud.ResourcePolicyPrincipalTypeFederated}) {
		t.Fatalf("Fed PrincipalTypes = %#v, want [federated]", fed.PrincipalTypes)
	}
	if len(fed.PrincipalARNs) != 0 || len(fed.PrincipalAccountIDs) != 0 {
		t.Fatalf("Fed must not emit ARNs/accountIDs for Federated: arns=%v accounts=%v", fed.PrincipalARNs, fed.PrincipalAccountIDs)
	}
	if fed.IsPublic || fed.IsCrossAccount {
		t.Fatalf("Fed: is_public=%v is_cross_account=%v, want both false", fed.IsPublic, fed.IsCrossAccount)
	}
}

// TestDeriveBucketPolicyResourcePermissionStatementsMalformedEffectIsSkipped
// proves that a statement with an unrecognized Effect value is silently dropped
// by normalizeStatementEffect, so only well-formed Allow/Deny entries are emitted.
func TestDeriveBucketPolicyResourcePermissionStatementsMalformedEffectIsSkipped(t *testing.T) {
	document := `{"Statement":[
		{"Sid":"Good","Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"*"},
		{"Sid":"Bad","Effect":"Permit","Principal":"*","Action":"s3:GetObject","Resource":"*"}
	]}`
	statements, err := deriveBucketPolicyResourcePermissionStatements(document, "123456789012")
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(statements) != 1 {
		t.Fatalf("statement count = %d, want 1 (malformed effect must be skipped): %#v", len(statements), statements)
	}
	if statements[0].StatementSID != "Good" {
		t.Fatalf("StatementSID = %q, want Good", statements[0].StatementSID)
	}
}

func statementBySID(t *testing.T, statements []s3service.ResourcePolicyStatement, sid string) s3service.ResourcePolicyStatement {
	t.Helper()
	for _, statement := range statements {
		if statement.StatementSID == sid {
			return statement
		}
	}
	t.Fatalf("missing statement with sid %q in %#v", sid, statements)
	return s3service.ResourcePolicyStatement{}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
