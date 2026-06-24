// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	kmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kms"
)

// TestClientListKeysDerivesResourcePolicyStatements proves the adapter reads the
// key policy (GetKeyPolicy) once per policy name and derives normalized
// metadata-only statements onto the Key, while never retaining the raw policy
// document on the scanner-owned type.
func TestClientListKeysDerivesResourcePolicyStatements(t *testing.T) {
	keyID := "1234abcd-12ab-34cd-56ef-1234567890ab"
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/" + keyID
	api := &fakeKMSAPI{
		listKeysPages: []*awskms.ListKeysOutput{{
			Keys: []kmstypes.KeyListEntry{{KeyId: aws.String(keyID), KeyArn: aws.String(keyARN)}},
		}},
		describeKey: map[string]*kmstypes.KeyMetadata{
			keyID: {KeyId: aws.String(keyID), Arn: aws.String(keyARN), KeyManager: kmstypes.KeyManagerTypeCustomer},
		},
		listPoliciesByKey: map[string][]*awskms.ListKeyPoliciesOutput{
			keyID: {{PolicyNames: []string{"default"}}},
		},
		keyPolicyByKey: map[string]string{
			keyID: `{"Statement":[
				{"Sid":"AllowPartnerDecrypt","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::999988887777:role/partner"},"Action":"kms:Decrypt","Resource":"*","Condition":{"StringEquals":{"kms:ViaService":"s3.amazonaws.com"}}}]}`,
		},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	}

	keys, err := adapter.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys() error = %v, want nil", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	statements := keys[0].ResourcePolicyStatements
	if len(statements) != 1 {
		t.Fatalf("ResourcePolicyStatements = %#v, want one derived statement", statements)
	}
	stmt := statements[0]
	if stmt.Effect != "Allow" || !stmt.IsCrossAccount {
		t.Fatalf("statement = %#v, want Allow + cross-account", stmt)
	}
	if !equalStrings(stmt.PrincipalAccountIDs, []string{"999988887777"}) {
		t.Fatalf("principal_account_ids = %#v", stmt.PrincipalAccountIDs)
	}
	if !equalStrings(stmt.ConditionKeys, []string{"kms:ViaService"}) {
		t.Fatalf("condition_keys = %#v, want [kms:ViaService] (names only)", stmt.ConditionKeys)
	}
	if !equalStrings(stmt.ConditionOperators, []string{"StringEquals"}) {
		t.Fatalf("condition_operators = %#v, want [StringEquals]", stmt.ConditionOperators)
	}
	if api.getKeyPolicyCalls != 1 {
		t.Fatalf("GetKeyPolicy calls = %d, want 1 (one read per policy name)", api.getKeyPolicyCalls)
	}
}

func TestDeriveKeyPolicyResourcePermissionStatements(t *testing.T) {
	document := `{"Version":"2012-10-17","Id":"key-default","Statement":[
		{"Sid":"EnableRoot","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"kms:*","Resource":"*"},
		{"Sid":"AllowPartnerDecrypt","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::999988887777:role/partner"},"Action":["kms:Decrypt","kms:DescribeKey"],"Resource":"*","Condition":{"StringEquals":{"kms:ViaService":"s3.us-east-1.amazonaws.com"}}},
		{"Sid":"AllowService","Effect":"Allow","Principal":{"Service":"cloudtrail.amazonaws.com"},"Action":"kms:GenerateDataKey","Resource":"*"}]}`

	statements, err := deriveKeyPolicyResourcePermissionStatements(document, "123456789012")
	if err != nil {
		t.Fatalf("deriveKeyPolicyResourcePermissionStatements() error = %v, want nil", err)
	}
	if len(statements) != 3 {
		t.Fatalf("statement count = %d, want 3: %#v", len(statements), statements)
	}

	root := statementBySID(t, statements, "EnableRoot")
	if root.IsCrossAccount {
		t.Fatalf("EnableRoot is_cross_account = true, want false (same account)")
	}
	if !equalStrings(root.PrincipalAccountIDs, []string{"123456789012"}) {
		t.Fatalf("EnableRoot principal_account_ids = %#v", root.PrincipalAccountIDs)
	}

	partner := statementBySID(t, statements, "AllowPartnerDecrypt")
	if !partner.IsCrossAccount {
		t.Fatalf("AllowPartnerDecrypt is_cross_account = false, want true")
	}
	if !equalStrings(partner.Actions, []string{"kms:Decrypt", "kms:DescribeKey"}) {
		t.Fatalf("AllowPartnerDecrypt actions = %#v", partner.Actions)
	}
	// Condition KEY only, never the value "s3.us-east-1.amazonaws.com".
	if !equalStrings(partner.ConditionKeys, []string{"kms:ViaService"}) {
		t.Fatalf("AllowPartnerDecrypt condition_keys = %#v, want [kms:ViaService]", partner.ConditionKeys)
	}
	if !equalStrings(partner.ConditionOperators, []string{"StringEquals"}) {
		t.Fatalf("AllowPartnerDecrypt condition_operators = %#v, want [StringEquals]", partner.ConditionOperators)
	}

	service := statementBySID(t, statements, "AllowService")
	if !equalStrings(service.PrincipalTypes, []string{awscloud.ResourcePolicyPrincipalTypeService}) {
		t.Fatalf("AllowService principal_types = %#v, want [service]", service.PrincipalTypes)
	}
	if len(service.PrincipalAccountIDs) != 0 {
		t.Fatalf("AllowService principal_account_ids = %#v, want empty for a service principal", service.PrincipalAccountIDs)
	}
}

func TestDeriveKeyPolicyResourcePermissionStatementsEmptyAndMalformed(t *testing.T) {
	empty, err := deriveKeyPolicyResourcePermissionStatements("", "123456789012")
	if err != nil {
		t.Fatalf("empty document error = %v, want nil", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty document statements = %#v, want none", empty)
	}
	if _, err := deriveKeyPolicyResourcePermissionStatements("{not json", "123456789012"); err == nil {
		t.Fatalf("malformed document error = nil, want parse error")
	}
}

func statementBySID(t *testing.T, statements []kmsservice.ResourcePolicyStatement, sid string) kmsservice.ResourcePolicyStatement {
	t.Helper()
	for _, statement := range statements {
		if statement.StatementSID == sid {
			return statement
		}
	}
	t.Fatalf("missing statement with sid %q in %#v", sid, statements)
	return kmsservice.ResourcePolicyStatement{}
}
