// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// wildcardAndCloudFrontARNs carry the two non-numeric account fields AWS
// itself writes: the policy wildcard "*" and CloudFront's legacy origin
// access identity principal account "cloudfront". (An ARN with a "*"
// region, arn:aws:ec2:*:*:..., is not an arn candidate at all: the region
// field of the scan is [a-z0-9-]*.)
var wildcardAndCloudFrontARNs = []string{
	"arn:aws:ec2:*:*:instance/*",
	"arn:aws:iam::*:role/*",
	"arn:aws:logs:us-east-1:*:log-group:*",
	"arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity E2EXAMPLE1ABC",
}

// TestARNWildcardAndCloudFrontAccountsAreKept: record mode never learns
// "*" or "cloudfront" in an ARN's account position, so both are written
// verbatim in every ARN field.
func TestARNWildcardAndCloudFrontAccountsAreKept(t *testing.T) {
	key := mustKey(t, keyA)
	resources := make([]any, 0, 3)
	for _, arn := range wildcardAndCloudFrontARNs[:3] {
		resources = append(resources, arn)
	}
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
			"resources":      resources,
			"principal_arns": []any{wildcardAndCloudFrontARNs[3]},
		}),
	}}, key, recordpolicy.Policy())
	payload := envs[0][0].Payload
	gotResources, _ := payload["resources"].([]any)
	gotPrincipals, _ := payload["principal_arns"].([]any)
	got := append(append([]any{}, gotResources...), gotPrincipals...)
	if len(got) != len(wildcardAndCloudFrontARNs) {
		t.Fatalf("got %d ARNs back, want %d", len(got), len(wildcardAndCloudFrontARNs))
	}
	for i, want := range wildcardAndCloudFrontARNs {
		account := strings.SplitN(fmt.Sprint(got[i]), ":", 6)[4]
		wantAccount := strings.SplitN(want, ":", 6)[4]
		if account != wantAccount {
			t.Errorf("ARN %d: account field rewritten to shape %q", i, shapeOf(account))
		}
	}
}

// TestVerifyAdmitsWildcardAndCloudFrontAccounts: an ARN whose account
// field is exactly "*" or exactly "cloudfront" is not private data; a raw
// 12-digit account and any other word stay refused.
func TestVerifyAdmitsWildcardAndCloudFrontAccounts(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("{")
	for i, arn := range wildcardAndCloudFrontARNs {
		fmt.Fprintf(&doc, `"a%d":%q,`, i, arn)
	}
	doc.WriteString(`"end":true}`)
	if err := recordpseudo.Verify([]byte(doc.String()), recordpseudo.Set{}); err != nil {
		t.Fatalf("wildcard or cloudfront ARN accounts refused: %v", err)
	}
	for _, plant := range []string{
		"arn:aws:iam::" + plantedAccount + ":role/*",
		"arn:aws:iam::acmecorp:user/example",
		"arn:aws:iam::cloudfronts:user/example",
		"arn:aws:iam::**:role/example",
	} {
		err := recordpseudo.Verify([]byte(`{"a":"`+plant+`"}`), recordpseudo.Set{})
		if err == nil || !strings.Contains(err.Error(), "alternative arn") {
			t.Errorf("ARN with account shape %q passed Verify: %v", shapeOf(strings.SplitN(plant, ":", 6)[4]), err)
		}
	}
}
