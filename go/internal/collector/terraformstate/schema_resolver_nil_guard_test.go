// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestNewPackagedSchemaResolverRejectsAnEmptyBundle pins the #5870 guard.
//
// LoadPackagedSchemaResolver used to return (nil, nil) here — success carrying
// a nil resolver. Nothing upstream turned that into a failure, so the collector
// started with SchemaResolver == nil, and schemaTrust answers SchemaUnknown for
// every (resourceType, attributeKey) pair against a nil resolver. Under
// SchemaUnknown the rules fail closed and every scalar becomes a redaction map,
// including "arn" — which is not a value but the join key into stateByARN.
//
// Reaching this state means neither the operator's schema directory nor the
// binary's own embedded bundle produced a single attribute. That is a broken
// build or a broken deployment, not a degraded mode worth running in, so it is
// now an error the caller cannot ignore.
func TestNewPackagedSchemaResolverRejectsAnEmptyBundle(t *testing.T) {
	t.Parallel()

	resolver, err := newPackagedSchemaResolver(map[string]map[string]struct{}{}, "/schemas")
	if err == nil {
		t.Fatal("newPackagedSchemaResolver(empty) error = nil, want an error rather than a nil resolver")
	}
	if resolver != nil {
		t.Fatalf("newPackagedSchemaResolver(empty) resolver = %#v, want nil alongside the error", resolver)
	}
	if got := err.Error(); !strings.Contains(got, "/schemas") {
		t.Errorf("error = %q, want it to name the schema directory an operator would check", got)
	}
}

// TestNewPackagedSchemaResolverAcceptsANonEmptyBundle keeps the guard from
// rejecting a bundle that did load.
func TestNewPackagedSchemaResolverAcceptsANonEmptyBundle(t *testing.T) {
	t.Parallel()

	resolver, err := newPackagedSchemaResolver(map[string]map[string]struct{}{
		"aws_s3_bucket": {"acl": struct{}{}},
	}, "/schemas")
	if err != nil {
		t.Fatalf("newPackagedSchemaResolver(non-empty) error = %v, want nil", err)
	}
	if resolver == nil {
		t.Fatal("newPackagedSchemaResolver(non-empty) = nil, want a resolver")
	}
	if !resolver.HasAttribute("aws_s3_bucket", "acl") {
		t.Error("HasAttribute(aws_s3_bucket, acl) = false, want true")
	}
}

// TestLoadPackagedSchemaResolverNeverReturnsANilResolverWithoutAnError is the
// contract the production wiring depends on, asserted at the public boundary
// and with the worst input an operator can supply: a schema directory that does
// not exist. The embedded bundle must carry the load, and the function must
// never hand back (nil, nil) for the collector to start on.
func TestLoadPackagedSchemaResolverNeverReturnsANilResolverWithoutAnError(t *testing.T) {
	t.Parallel()

	resolver, err := LoadPackagedSchemaResolver(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadPackagedSchemaResolver() error = %v, want nil (the embedded bundle covers a missing dir)", err)
	}
	if resolver == nil {
		t.Fatal("LoadPackagedSchemaResolver() = (nil, nil): a nil resolver fail-closed-redacts every attribute, arn included")
	}
}
