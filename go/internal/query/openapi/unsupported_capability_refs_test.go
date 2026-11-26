// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// notImplementedResponseRef is the shared 501 response for a capability the
// runtime profile does not support. The served spec must never reference the
// undefined "#/components/responses/UnsupportedCapability" fragment.
const notImplementedResponseRef = "#/components/responses/NotImplemented"

// TestOpenAPISpecHasNoUnsupportedCapabilityRef is the shared-schema accuracy
// pin: the audit found two 501 responses referencing the undefined
// "#/components/responses/UnsupportedCapability" fragment, which leaves the
// served document with a dangling $ref. No occurrence may remain.
func TestOpenAPISpecHasNoUnsupportedCapabilityRef(t *testing.T) {
	t.Parallel()

	if strings.Contains(Spec(), "UnsupportedCapability") {
		t.Fatal("served Spec() still references UnsupportedCapability; 501s must use #/components/responses/NotImplemented")
	}
}

// TestOpenAPI501sResolveToNotImplemented pins the two audited 501 responses
// to the shared NotImplemented response and proves the target exists, so the
// refs resolve.
func TestOpenAPI501sResolveToNotImplemented(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(Spec()) error = %v, want nil", err)
	}
	paths, _ := spec["paths"].(map[string]any)
	for _, tc := range []struct{ path, method string }{
		{"/api/v0/code/routes/callers", "post"},
		{"/api/v0/evidence/admission-decisions", "get"},
	} {
		item, ok := paths[tc.path].(map[string]any)
		if !ok {
			t.Fatalf("paths[%q] missing from served Spec()", tc.path)
		}
		op, ok := item[tc.method].(map[string]any)
		if !ok {
			t.Fatalf("paths[%q][%q] missing from served Spec()", tc.path, tc.method)
		}
		responses, _ := op["responses"].(map[string]any)
		response, _ := responses["501"].(map[string]any)
		if ref, _ := response["$ref"].(string); ref != notImplementedResponseRef {
			t.Fatalf("paths[%q][%q] 501 $ref = %q, want %q", tc.path, tc.method, ref, notImplementedResponseRef)
		}
	}
	components, _ := spec["components"].(map[string]any)
	responses, _ := components["responses"].(map[string]any)
	if _, ok := responses["NotImplemented"].(map[string]any); !ok {
		t.Fatal("components.responses.NotImplemented missing from served Spec()")
	}
}

// TestSemanticEvidenceRowTruthBasisEnum pins the widened truth_basis enum:
// the two reachable kinds plus the semanticEvidenceTruthBasis default arm,
// which is unreachable on current filters but producible by the owner. A
// removed or reordered value fails closed here instead of silently narrowing
// the contract.
func TestSemanticEvidenceRowTruthBasisEnum(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(Spec()) error = %v, want nil", err)
	}
	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	row, _ := schemas["SemanticEvidenceRow"].(map[string]any)
	props, _ := row["properties"].(map[string]any)
	basis, _ := props["truth_basis"].(map[string]any)
	raw, ok := basis["enum"].([]any)
	if !ok {
		t.Fatal("SemanticEvidenceRow.properties.truth_basis has no enum list")
	}
	var got []string
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("truth_basis enum holds non-string %v", v)
		}
		got = append(got, s)
	}
	want := []string{"semantic_observation", "code_hint", "semantic_evidence"}
	if len(got) != len(want) {
		t.Fatalf("truth_basis enum = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("truth_basis enum = %v, want %v", got, want)
		}
	}
}
