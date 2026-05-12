package terraformstate_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/terraformschema"
)

// TestLoadPackagedSchemaResolverCoversTier2Attributes is the regression guard
// that proves the packaged AWS schema actually carries the attribute keys the
// Tier-2 tfstate drift verifier depends on. If a future schema bump drops
// these attributes the build fails here instead of silently regressing the
// E2E drift bucket coverage.
func TestLoadPackagedSchemaResolverCoversTier2Attributes(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}

	tier2Attributes := []struct {
		resourceType string
		attributeKey string
	}{
		{"aws_s3_bucket", "acl"},
		{"aws_s3_bucket", "bucket"},
		{"aws_s3_bucket", "versioning"},
		{"aws_s3_bucket", "server_side_encryption_configuration"},
	}
	for _, attribute := range tier2Attributes {
		if !resolver.HasAttribute(attribute.resourceType, attribute.attributeKey) {
			t.Errorf("HasAttribute(%q, %q) = false, want true (Tier-2 drift verifier depends on this attribute)",
				attribute.resourceType, attribute.attributeKey)
		}
	}
}

// TestLoadPackagedSchemaResolverFallsBackToEmbeddedSchemas proves the
// container-safe contract: when the on-disk schema directory is unset or
// empty, the resolver still loads from the schemas embedded in the binary so
// runtime binaries (collector-terraform-state, future tooling) never lose
// attribute coverage just because their image lacks the schemas tree on
// disk.
func TestLoadPackagedSchemaResolverFallsBackToEmbeddedSchemas(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(t.TempDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver(empty dir) error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver(empty dir) = nil, want fallback to embedded schemas")
	}
	if !resolver.HasAttribute("aws_s3_bucket", "acl") {
		t.Error("HasAttribute(aws_s3_bucket, acl) = false, want true from embedded fallback")
	}

	resolver, err = terraformstate.LoadPackagedSchemaResolver("")
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver(blank) error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver(blank) = nil, want fallback to embedded schemas")
	}
	if !resolver.HasAttribute("aws_s3_bucket", "server_side_encryption_configuration") {
		t.Error("HasAttribute(aws_s3_bucket, server_side_encryption_configuration) = false, want true from embedded fallback")
	}
}
