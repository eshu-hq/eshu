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

// TestEnumFieldsAreNeverSubstituted (#6965 Phase 3): resource_type,
// target_type, relationship_type and service_kind hold collector-defined
// enum values, never customer data. A tag value that happens to equal one
// of their words ("queue") was learned and rewrote aws_sqs_queue into
// aws_sqs_t<hex> in a real recording, projecting the queue under the wrong
// type. Enum fields pass through verbatim.
func TestEnumFieldsAreNeverSubstituted(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:sqs", nil, map[string]any{
		"resource_type":     "aws_sqs_queue",
		"target_type":       "aws_sqs_queue",
		"relationship_type": "sqs_queue_uses_dead_letter_queue",
		"service_kind":      "sqs",
		"tags":              map[string]any{"Kind": "queue"},
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	p := envs[0][0].Payload
	for field, want := range map[string]string{
		"resource_type":     "aws_sqs_queue",
		"target_type":       "aws_sqs_queue",
		"relationship_type": "sqs_queue_uses_dead_letter_queue",
		"service_kind":      "sqs",
	} {
		if got := fmt.Sprint(p[field]); got != want {
			t.Errorf("%s = %q, want %q verbatim", field, got, want)
		}
	}
}

// TestEnumFieldsPseudonymizeCustomerTypes (#6965 review R1): CloudFormation
// stack resources set target_type to the resource type, which can be a
// customer-named Custom::<name> or a private registry <Org>::Svc::Res. Only
// enum-shaped values (snake_case, or AWS::Service::Resource) are verbatim;
// a customer-named type is pseudonymized per :: component.
func TestEnumFieldsPseudonymizeCustomerTypes(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:cloudformation", nil, map[string]any{
		"resource_type": "AWS::SQS::Queue",
		"target_type":   "Custom::ZyxcorpBillingHook",
		"service_kind":  "cloudformation",
	})
	gen2 := generation("aws:"+acct+":us-east-1:cloudformation", nil, map[string]any{
		"target_type": "Zyxcorp::Payments::Ledger",
	})
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen, gen2}}, key, recordpolicy.Policy())
	if got := fmt.Sprint(envs[0][0].Payload["resource_type"]); got != "AWS::SQS::Queue" {
		t.Errorf("AWS type rewritten: %q", got)
	}
	for _, p := range []map[string]any{envs[0][0].Payload, envs[1][0].Payload} {
		got := fmt.Sprint(p["target_type"])
		if strings.Contains(got, "Zyxcorp") || strings.Contains(got, "BillingHook") || strings.Contains(got, "Payments") || strings.Contains(got, "Ledger") {
			t.Errorf("customer-named type survived raw: %q", got)
		}
	}
}
