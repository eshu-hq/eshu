package terraformstate_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserAddsResourceCorrelationAnchors(t *testing.T) {
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

	anchors, ok := resource.Payload["correlation_anchors"].([]any)
	if !ok {
		t.Fatalf("correlation_anchors = %#v, want []any", resource.Payload["correlation_anchors"])
	}
	if got, want := len(anchors), 5; got != want {
		t.Fatalf("correlation anchor count = %d, want %d: %#v", got, want, anchors)
	}
	for _, kind := range []string{"account_id", "arn", "id", "name", "region"} {
		anchor := anchorByKind(t, anchors, kind)
		if got, ok := anchor["value_hash"].(string); !ok || len(got) != 64 {
			t.Fatalf("%s value_hash = %#v, want sha256 hex", kind, anchor["value_hash"])
		}
	}
	if anchorContainsKind(anchors, "password") {
		t.Fatalf("password anchor emitted in %#v", anchors)
	}
	assertNoRawSecret(t, result, "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0")
	assertNoRawSecret(t, result, "i-1234567890abcdef0")
	assertNoRawSecret(t, result, "123456789012")
	assertNoRawSecret(t, result, "super-secret")
}

func anchorByKind(t *testing.T, anchors []any, kind string) map[string]any {
	t.Helper()

	for _, anchor := range anchors {
		typed, ok := anchor.(map[string]any)
		if !ok {
			t.Fatalf("anchor = %#v, want map[string]any", anchor)
		}
		if typed["anchor_kind"] == kind {
			return typed
		}
	}
	t.Fatalf("missing anchor kind %q in %#v", kind, anchors)
	return nil
}

func anchorContainsKind(anchors []any, kind string) bool {
	for _, anchor := range anchors {
		typed, ok := anchor.(map[string]any)
		if ok && typed["anchor_kind"] == kind {
			return true
		}
	}
	return false
}
