package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// stubProviderSchemaResolver returns true only for the (resourceType,
// attributeKey) pairs explicitly registered. Tests use it to prove the parser
// honors per-attribute schema trust at the classification boundary.
type stubProviderSchemaResolver struct {
	known map[string]map[string]struct{}
}

func newStubResolver(pairs ...[2]string) *stubProviderSchemaResolver {
	resolver := &stubProviderSchemaResolver{known: map[string]map[string]struct{}{}}
	for _, pair := range pairs {
		resourceType, attributeKey := pair[0], pair[1]
		if _, ok := resolver.known[resourceType]; !ok {
			resolver.known[resourceType] = map[string]struct{}{}
		}
		resolver.known[resourceType][attributeKey] = struct{}{}
	}
	return resolver
}

func (s *stubProviderSchemaResolver) HasAttribute(resourceType string, attributeKey string) bool {
	attrs, ok := s.known[resourceType]
	if !ok {
		return false
	}
	_, ok = attrs[attributeKey]
	return ok
}

// TestParserPreservesAttributesWhenSchemaResolverKnowsResourceType is the
// load-bearing Scope 3 proof: with a SchemaResolver loaded, a non-sensitive
// attribute on a known resource type passes through to the resource fact
// unredacted so downstream drift detection can compare config vs state truth.
//
// Without this wiring, attributes.go classifies every attribute as
// SchemaUnknown, which fails closed (scalars HMAC-stomped, composites dropped)
// and prevents the Tier-2 verifier from observing aws_s3_bucket.acl drift.
func TestParserPreservesAttributesWhenSchemaResolverKnowsResourceType(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "acl"},
		[2]string{"aws_s3_bucket", "bucket"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"primary",
			"instances":[{
				"attributes":{
					"acl":"private",
					"bucket":"my-bucket"
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	if got, want := attributes["acl"], "private"; got != want {
		t.Fatalf("attributes[acl] = %#v, want %q (SchemaKnown should preserve scalar)", got, want)
	}
	if got, want := attributes["bucket"], "my-bucket"; got != want {
		t.Fatalf("attributes[bucket] = %#v, want %q (SchemaKnown should preserve scalar)", got, want)
	}
}

// TestParserFailsClosedForUnknownAttributesEvenWithSchemaResolver guards the
// negative-case invariant: a SchemaResolver that DOES NOT register an
// attribute on a known resource type must still produce a fail-closed
// classification. Without this guarantee, a future regression that
// blanket-trusts every attribute would silently leak sensitive values.
func TestParserFailsClosedForUnknownAttributesEvenWithSchemaResolver(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = newStubResolver(
		[2]string{"aws_s3_bucket", "acl"},
	)

	state := `{
		"serial":17,
		"lineage":"lineage-123",
		"resources":[{
			"mode":"managed",
			"type":"aws_s3_bucket",
			"name":"primary",
			"instances":[{
				"attributes":{
					"acl":"private",
					"unmapped_future_attr":"future-value"
				}
			}]
		}]
	}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	resource := factByKind(t, result.Facts, facts.TerraformStateResourceFactKind)
	attributes, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource attributes = %#v, want map[string]any", resource.Payload["attributes"])
	}
	if got, want := attributes["acl"], "private"; got != want {
		t.Fatalf("attributes[acl] = %#v, want %q (known attribute preserved)", got, want)
	}
	marker, ok := attributes["unmapped_future_attr"].(map[string]any)
	if !ok {
		t.Fatalf("attributes[unmapped_future_attr] = %#v, want redaction marker map (fail-closed)", attributes["unmapped_future_attr"])
	}
	if value, _ := marker["marker"].(string); !strings.HasPrefix(value, "redacted:hmac-sha256:") {
		t.Fatalf("attributes[unmapped_future_attr] marker = %#v, want HMAC redaction marker", marker["marker"])
	}
}
