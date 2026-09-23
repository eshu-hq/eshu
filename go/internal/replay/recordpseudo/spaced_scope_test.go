// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestSpacedNameWordIsPseudonymizedInClassifiedFields (#6965 review M1): a
// word first learned from a spaced alarm name must still be pseudonymized
// when a classified field (name, a tag value) carries it, whatever the key
// order; only Keep values are exempt.
func TestSpacedNameWordIsPseudonymizedInClassifiedFields(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:cloudwatch", nil, map[string]any{
		"arn":  "arn:aws:cloudwatch:us-east-1:" + acct + ":alarm:Quorvex latency",
		"name": "Quorvex",
		"tags": map[string]any{"Owner": "Quorvex"},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	raw, _ := json.Marshal(envs[0][0].Payload)
	if strings.Contains(string(raw), "Quorvex") {
		t.Errorf("a word learned from an alarm name stayed raw in a classified field: %s", raw)
	}
}

// TestCloudFrontPrincipalPhraseSurvivesAlarmWords (#6965 review M2): a
// customer alarm whose name contains the AWS phrase's words must not rewrite
// the AWS-owned phrase in the CloudFront principal of the same recording.
func TestCloudFrontPrincipalPhraseSurvivesAlarmWords(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:s3", nil, map[string]any{
		"resources": []any{
			"arn:aws:cloudwatch:us-east-1:" + acct + ":alarm:Origin Access Identity drift",
			"arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity E1ABCD23EFGH4I",
		},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	resources, _ := envs[0][0].Payload["resources"].([]any)
	principal := fmt.Sprint(resources[1])
	if !strings.HasPrefix(principal, "arn:aws:iam::cloudfront:user/CloudFront Origin Access Identity ") {
		t.Errorf("AWS phrase rewritten by alarm words: %s", principal)
	}
	if strings.Contains(principal, "E1ABCD23EFGH4I") {
		t.Errorf("OAI id raw: %s", principal)
	}
}

// TestAlexaTypesAreAWSOwned (#6965 review M5): Alexa::ASK::Skill is an
// AWS-owned CloudFormation type outside AWS::; it stays verbatim and its
// words never rewrite Keep fields.
func TestAlexaTypesAreAWSOwned(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:cloudformation", nil, map[string]any{
		"resource_type": "Alexa::ASK::Skill",
		"state":         "Skill",
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	p := envs[0][0].Payload
	if got := fmt.Sprint(p["resource_type"]); got != "Alexa::ASK::Skill" {
		t.Errorf("resource_type = %q, want verbatim", got)
	}
	if got := fmt.Sprint(p["state"]); got != "Skill" {
		t.Errorf("state = %q, want verbatim", got)
	}
}
