// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestSpacedARNComponentIDsArePseudonymized (#6965 Phase 3): the CloudFront
// origin access identity principal is
// arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity <ID>. The
// resource component carries spaces, so it was never learned and the
// account-issued OAI ID reached the cassette raw; no Verify alternative
// covers its shape. The ID-shaped token is pseudonymized, and AWS's fixed
// phrase stays readable.
func TestSpacedARNComponentIDsArePseudonymized(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:s3", nil, map[string]any{
		"principal_arns": []any{
			"arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity E1ABCD23EFGH4I",
			// An issued ID can be all letters; the uppercase run is the ID.
			"arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity EQWERTYUIOPAS",
		},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	raw, err := json.Marshal(envs[0][0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	for _, id := range []string{"E1ABCD23EFGH4I", "EQWERTYUIOPAS"} {
		if strings.Contains(out, id) {
			t.Errorf("origin access identity ID %s survived raw: %s", id, out)
		}
	}
	if !strings.Contains(out, "arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity ") {
		t.Errorf("AWS's fixed principal phrase was rewritten: %s", out)
	}
}

// TestSpacedCustomerARNNamesArePseudonymized (#6965 review R2): a spaced
// ARN component is not always an AWS phrase. CloudWatch alarm ARNs carry
// the customer's free-text alarm name. Every word of it is pseudonymized,
// even when no other field names the alarm.
func TestSpacedCustomerARNNamesArePseudonymized(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:cloudwatch", nil, map[string]any{
		"resources": []any{"arn:aws:cloudwatch:us-east-1:" + acct + ":alarm:Zyxcorp Payments Latency"},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	raw, err := json.Marshal(envs[0][0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"Zyxcorp", "Payments", "Latency"} {
		if strings.Contains(string(raw), word) {
			t.Errorf("alarm-name word %q survived raw: %s", word, raw)
		}
	}
	if !strings.Contains(string(raw), ":alarm:") {
		t.Errorf("alarm type token lost: %s", raw)
	}
}

// TestAWSPhraseOnlyInTheCloudFrontPrincipal (#6965 review N2): the AWS
// phrase keeps its words only in the IAM principal owned by the cloudfront
// account. A customer alarm named with the same words is free text.
func TestAWSPhraseOnlyInTheCloudFrontPrincipal(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:cloudwatch", nil, map[string]any{
		"resources": []any{"arn:aws:cloudwatch:us-east-1:" + acct + ":alarm:CloudFront Origin Access Identity Plavorn Outage"},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	raw, err := json.Marshal(envs[0][0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"Plavorn", "Outage"} {
		if strings.Contains(string(raw), word) {
			t.Errorf("alarm word %q survived behind the AWS phrase: %s", word, raw)
		}
	}
}
