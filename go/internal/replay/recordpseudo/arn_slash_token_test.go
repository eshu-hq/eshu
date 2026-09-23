// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// TestSlashLedARNTypeTokensSurvive (#6987 gating re-review P1): services
// whose ARN resource starts with "/" (API Gateway, Grafana, Signer,
// Keyspaces, EMR Serverless) carry their type token as the first
// component once the ARN is split, so the vocabulary must list it without
// the slash. The type token survives verbatim, the customer id after it is
// pseudonymized, and nothing is reported as an unlisted type.
func TestSlashLedARNTypeTokensSurvive(t *testing.T) {
	key := mustKey(t, keyA)
	arns := []string{
		"arn:aws:apigateway:us-east-1::/restapis/abc123/stages/prod",
		"arn:aws:grafana:us-east-1:" + acct + ":/workspaces/g-1a2b3c",
		"arn:aws:signer:us-east-1:" + acct + ":/signing-profiles/acmeprofile",
		"arn:aws:cassandra:us-east-1:" + acct + ":/keyspace/acmekeyspace/",
		"arn:aws:emr-serverless:us-east-1:" + acct + ":/applications/00f1abcd",
	}
	resources := make([]any, len(arns))
	for i, a := range arns {
		resources[i] = a
	}
	_, envs, report := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{"resources": resources}),
	}}, key, recordpolicy.Policy())
	got, _ := envs[0][0].Payload["resources"].([]any)
	for i, want := range []string{"/restapis/", "/workspaces/", "/signing-profiles/", "/keyspace/", "/applications/"} {
		if s := fmt.Sprint(got[i]); !strings.Contains(s, want) {
			t.Errorf("type token %q lost: %s", want, s)
		}
	}
	for _, raw := range []string{"abc123", "g-1a2b3c", "acmeprofile", "acmekeyspace", "00f1abcd"} {
		if strings.Contains(fmt.Sprint(got), raw) {
			t.Errorf("customer id %q survived raw", raw)
		}
	}
	if len(report.UnlistedARNTypes) != 0 {
		t.Errorf("slash-led type tokens reported as unlisted: %v", report.UnlistedARNTypes)
	}
}
