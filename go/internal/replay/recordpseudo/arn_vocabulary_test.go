// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
)

// Review finding F1/F7 on 40a4ac36e (verdict-p3.md). Every literal is synthetic.

// TestARNUnknownFirstComponentIsLearned (F1): the first resource component
// is a type token only when the per-service vocabulary says so. An SNS
// subscription ARN, a policy resource on a topic, and any unknown first
// component carry a customer name that must be pseudonymized.
func TestARNUnknownFirstComponentIsLearned(t *testing.T) {
	key := mustKey(t, keyA)
	pseudoAcct := `0000[0-9]{8}`
	hexName := `n[0-9a-f]{11}`
	cases := []struct{ arn, want, raw string }{
		{"arn:aws:sns:us-east-1:" + acct + ":acme-alerts:3f0c2a4e-1b7d-4c6e-9a1f-0d2e3c4b5a69", `^arn:aws:sns:us-east-1:` + pseudoAcct + `:` + hexName + `:` + hexName + `$`, "acme-alerts"},
		{"arn:aws:sns:us-east-1:" + acct + ":acme-alerts:*", `^arn:aws:sns:us-east-1:` + pseudoAcct + `:` + hexName + `:\*$`, "acme-alerts"},
		{"arn:aws:sqs:us-east-1:" + acct + ":acme-orders-queue", `^arn:aws:sqs:us-east-1:` + pseudoAcct + `:` + hexName + `$`, "acme-orders-queue"},
		{"arn:aws:iam::" + acct + ":root", `^arn:aws:iam::` + pseudoAcct + `:root$`, ""},
		{"arn:aws:iam::" + acct + ":role/acme-deployer", `^arn:aws:iam::` + pseudoAcct + `:role/` + hexName + `$`, "acme-deployer"},
		{"arn:aws:lambda:us-east-1:" + acct + ":function:acme-resizer:prod", `^arn:aws:lambda:us-east-1:` + pseudoAcct + `:function:` + hexName + `:prod$`, "acme-resizer"},
		{"arn:aws:unknownsvc:us-east-1:" + acct + ":acme-thing/child", `^arn:aws:unknownsvc:us-east-1:` + pseudoAcct + `:` + hexName + `/` + hexName + `$`, "acme-thing"},
	}
	for _, tc := range cases {
		got := pseudonymOf(t, key, "arn", tc.arn)
		if !regexp.MustCompile(tc.want).MatchString(got) {
			t.Errorf("ARN shape %q -> %q does not match %s", shapeOf(tc.arn), shapeOf(got), tc.want)
		}
		if tc.raw != "" && strings.Contains(got, tc.raw) {
			t.Errorf("raw first component survived in an ARN of shape %q", shapeOf(tc.arn))
		}
	}
	// The policy resources list is where the live IAM scanner puts these.
	payloads, _ := wrapAll(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
			"resources": []any{"arn:aws:sns:us-east-1:" + acct + ":acme-alerts:*", "arn:aws:s3:::acme-bucket/*"},
		}),
	}}, key, recordpolicy.Policy())
	if rendered := fmt.Sprint(payloads[0]["resources"]); strings.Contains(rendered, "acme") {
		t.Errorf("resources[] carries a raw name")
	}
}
