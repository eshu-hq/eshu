package terraformstate_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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
	assertNoRawSecret(t, result, "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0")
	assertNoRawSecret(t, result, "i-1234567890abcdef0")
	assertNoRawSecret(t, result, "123456789012")
	assertNoRawSecret(t, result, "super-secret")
}
