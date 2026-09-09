// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestBuildProviderConfigWriteAcceptsDocumentedKinds proves the write path
// accepts every kind in the canonical accepted-kind list and rejects an
// unknown one, so the list cannot silently claim a kind the code rejects or
// omit one it accepts.
//
// This is the builder half of the provider-kind lockstep (issue #5166): the
// OpenAPI-enum half lives in the external admin_test package
// (admin/openapi_test.go) because it reads the root's OpenAPISpec, and both
// halves read the one shared list in querytestutil. Add a new kind in the
// same change that adds its buildXProviderConfigWrite case, its spec enums,
// and the shared list entry.
func TestBuildProviderConfigWriteAcceptsDocumentedKinds(t *testing.T) {
	t.Parallel()

	// An empty body fails kind-specific field validation (missing client_id,
	// etc.) — that is fine; the point is it must NOT be the unknown-kind
	// rejection, i.e. the switch dispatched into a builder.
	const unknownKindMarker = "provider_kind must be"
	for _, kind := range querytestutil.AcceptedProviderConfigKinds {
		_, err := buildProviderConfigWrite(providerConfigWriteRequest{ProviderKind: kind})
		if err != nil && strings.Contains(err.Error(), unknownKindMarker) {
			t.Fatalf("buildProviderConfigWrite rejected accepted kind %q as unknown: %v", kind, err)
		}
	}
	if _, err := buildProviderConfigWrite(providerConfigWriteRequest{ProviderKind: "__never_a_real_kind__"}); err == nil ||
		!strings.Contains(err.Error(), unknownKindMarker) {
		t.Fatalf("buildProviderConfigWrite(bogus kind) error = %v, want an unknown-kind rejection containing %q", err, unknownKindMarker)
	}
}
