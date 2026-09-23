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

// TestUnlistedARNTypeTokensAreReported (P2): a listed service whose ARN
// leads with a token outside its vocabulary is counted and named as
// service:token in the report; a service with no vocabulary (SNS) is a
// customer name and is never reported.
func TestUnlistedARNTypeTokensAreReported(t *testing.T) {
	key := mustKey(t, keyA)
	_, _, report := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:iam", nil, map[string]any{
			"resources": []any{
				"arn:aws:lambda:us-east-1:" + acct + ":widget/orders-widget",
				"arn:aws:lambda:us-east-1:" + acct + ":widget/billing-widget",
				"arn:aws:sns:us-east-1:" + acct + ":order-events",
				"arn:aws:lambda:us-east-1:" + acct + ":function:img-resizer",
			},
		}),
	}}, key, recordpolicy.Policy())
	if strings.Join(report.UnlistedARNTypes, ",") != "lambda:widget" || report.UnlistedARNTypeCount != 2 {
		t.Errorf("unlisted ARN types = %v (count %d), want [lambda:widget] count 2", report.UnlistedARNTypes, report.UnlistedARNTypeCount)
	}
	rendered := fmt.Sprint(report.LogAttrs())
	if !strings.Contains(rendered, "unlisted_arn_types") || !strings.Contains(rendered, "lambda:widget") {
		t.Errorf("log attrs do not carry the unlisted ARN type")
	}
	for _, raw := range []string{"order-events", "orders-widget", "billing-widget", "img-resizer"} {
		if strings.Contains(rendered, raw) {
			t.Errorf("log attrs carry a customer name of length %d", len(raw))
		}
	}
}
