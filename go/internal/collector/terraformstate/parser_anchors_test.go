// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestParserOmitsCorrelationAnchorsForRedactedAttributes pins the anchor
// guarantee: an unknown provider schema publishes no correlation anchors.
//
// That guarantee is decided by redactsAnchor, which calls the BARE schemaTrust,
// and #5870's identity-join-key exemption deliberately does not touch it. The
// join is rescued through the attribute the drift loader actually reads, not by
// widening what the anchor contract hands out.
//
// The value assertions below changed with #5870 and the split is the point:
//
//   - `arn` and `id` are now emitted RAW under an unknown schema, because a
//     downstream reader JOINS on them and a redaction marker there does not
//     withhold a value, it produces a wrong answer (the row leaves the join and
//     its resource is reported as an orphan).
//   - `account_id`, `name`, and `region` stay marked. Nothing joins on them.
//   - `password` stays marked through a different rule entirely: an
//     operator-declared sensitive key, which redact.RuleSet.Classify tests
//     BEFORE schema trust and which therefore outranks the exemption.
//
// The old test also asserted the bare account id "123456789012" never appears.
// That is unsatisfiable once `arn` is raw -- an ARN embeds the account id -- and
// keeping it would have meant asserting something the approved design
// contradicts. It is replaced by the shape assertion below, which still proves
// the standalone account_id attribute is withheld.
func TestParserOmitsCorrelationAnchorsForRedactedAttributes(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"mode":"managed",
		"type":"aws_instance",
		"name":"web",
		"instances":[{
			"attributes":{
				"arn":"arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
				"id":"i-1234567890abcdef0",
				"name":"web-prod",
				"region":"us-east-1",
				"account_id":"123456789012",
				"password":"super-secret",
				"metadata":{"ignored":"composite"}
			}
		}]
	}]}`

	result := parseFixtureFacts(t, state)
	resource := factByKind(t, result, facts.TerraformStateResourceFactKind)

	if _, ok := resource.Payload["correlation_anchors"]; ok {
		t.Fatalf("correlation_anchors = %#v, want omitted when source attributes are redacted", resource.Payload["correlation_anchors"])
	}

	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}

	for key, want := range map[string]string{
		"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
		"id":  "i-1234567890abcdef0",
	} {
		if got := attributes[key]; got != want {
			t.Fatalf("attributes[%q] = %#v, want %q raw: a join key must survive an unknown schema (#5870)", key, got, want)
		}
	}
	for _, key := range []string{"account_id", "name", "region", "password"} {
		marker, ok := attributes[key].(map[string]any)
		if !ok {
			t.Fatalf("attributes[%q] = %#v, want a redaction marker: only join keys are exempt", key, attributes[key])
		}
		if _, ok := marker["marker"]; !ok {
			t.Fatalf("attributes[%q] = %#v, want a redaction marker object", key, marker)
		}
	}

	// Still absolute: the operator-declared sensitive value never appears in any
	// emitted fact, whatever the schema trust says.
	assertNoRawSecret(t, result, "super-secret")
}
