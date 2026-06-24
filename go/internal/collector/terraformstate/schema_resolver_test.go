// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestLoadPackagedSchemaResolverCoversRemoteE2EDataSourceComposites proves
// the packaged resolver trusts Terraform data-source composites only when the
// shipped provider schema declares them. Terraform state serializes managed
// resources and data sources through the same "resources" array, so the parser
// receives only the resource type and attribute key at the composite boundary.
func TestLoadPackagedSchemaResolverCoversRemoteE2EDataSourceComposites(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}

	supportedAttributes := []struct {
		resourceType string
		attributeKey string
	}{
		{"aws_iam_policy_document", "statement"},
		{"aws_kms_key", "multi_region_configuration"},
		{"aws_kms_key", "xks_key_configuration"},
		{"aws_subnets", "filter"},
		{"aws_subnets", "ids"},
		{"aws_vpc", "cidr_block_associations"},
		{"aws_vpc", "filter"},
	}
	for _, attribute := range supportedAttributes {
		if !resolver.HasAttribute(attribute.resourceType, attribute.attributeKey) {
			t.Errorf("HasAttribute(%q, %q) = false, want true (remote E2E provider schema declares this data-source shape)",
				attribute.resourceType, attribute.attributeKey)
		}
	}
}

// TestLoadPackagedSchemaResolverLeavesUnsupportedRemoteE2EGapsUnknown locks
// the fail-closed side of #566: shapes observed in remote E2E are not promoted
// unless the packaged provider schema proves they are Terraform state evidence.
func TestLoadPackagedSchemaResolverLeavesUnsupportedRemoteE2EGapsUnknown(t *testing.T) {
	t.Parallel()

	resolver, err := terraformstate.LoadPackagedSchemaResolver(terraformschema.DefaultSchemaDir())
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = nil, want resolver loaded from packaged schemas")
	}

	unsupportedAttributes := []struct {
		resourceType string
		attributeKey string
	}{
		{"cloudinit_config", "part"},
	}
	for _, attribute := range unsupportedAttributes {
		if resolver.HasAttribute(attribute.resourceType, attribute.attributeKey) {
			t.Errorf("HasAttribute(%q, %q) = true, want false without packaged provider-schema proof",
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
	if !resolver.HasAttribute("aws_iam_policy_document", "statement") {
		t.Error("HasAttribute(aws_iam_policy_document, statement) = false, want true from embedded data-source fallback")
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
	if !resolver.HasAttribute("aws_vpc", "filter") {
		t.Error("HasAttribute(aws_vpc, filter) = false, want true from embedded data-source fallback")
	}
}
