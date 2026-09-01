// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"regexp"
	"testing"
)

// TestOpenAPIOperationIDsStayLowerCamelCase guards a wire contract that no
// other gate can see. operationId is a client-facing identifier: generated
// SDKs turn it into a method name, so changing one renames a caller's method
// without any Go-visible break. Every operationId in this spec is
// lowerCamelCase, and nothing in the Go build, vet, or test lanes objects if
// one stops being -- a package-wide identifier rename swept
// "investigateHardcodedSecrets" to "InvestigateHardcodedSecrets" in
// openapi_paths_code_security.go while the whole suite stayed green (#6060).
//
// This asserts over the assembled spec rather than the source files, because
// the assembled spec is what clients actually receive.
// lowerCamelCaseOperationID is the shape every operationId in this spec
// follows: a lowercase first letter and nothing but letters and digits after
// it. Checking only the first rune is not enough -- investigate_hardcoded_secrets
// starts lowercase and would pass, while still breaking the method name a
// generated client derives from it. All 242 operationIds match this today, so
// the strict form costs nothing and closes the gap.
var lowerCamelCaseOperationID = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

func TestOpenAPIOperationIDsStayLowerCamelCase(t *testing.T) {
	t.Parallel()

	ids := openAPIOperationIDs(t)
	if len(ids) == 0 {
		t.Fatal("openAPIOperationIDs() found no operationIds; the walk is broken, not the spec")
	}
	for path, byMethod := range ids {
		for method, id := range byMethod {
			if id == "" {
				t.Errorf("%s %s: empty operationId", method, path)
				continue
			}
			if !lowerCamelCaseOperationID.MatchString(id) {
				t.Errorf("%s %s: operationId = %q, want lowerCamelCase matching %s (a generated client turns this into a method name)", method, path, id, lowerCamelCaseOperationID)
			}
		}
	}
}

// TestOpenAPIOperationIDsAreUnique catches the other way a rename sweep breaks
// this contract: collapsing two operationIds into one. Generators key on
// operationId, so a duplicate silently drops or overwrites a client method.
func TestOpenAPIOperationIDsAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}
	for path, byMethod := range openAPIOperationIDs(t) {
		for method, id := range byMethod {
			where := method + " " + path
			if prior, dup := seen[id]; dup {
				t.Errorf("operationId %q used by both %s and %s; it must be unique across the spec", id, prior, where)
				continue
			}
			seen[id] = where
		}
	}
}

// TestOpenAPICodeSecuritySecretsInvestigateOperationID pins the specific
// operationId that a rename sweep already broke once, so the regression has a
// named test and not only the convention sweep above.
func TestOpenAPICodeSecuritySecretsInvestigateOperationID(t *testing.T) {
	t.Parallel()

	ids := openAPIOperationIDs(t)
	const route = "/api/v0/code/security/secrets/investigate"
	byMethod, ok := ids[route]
	if !ok {
		t.Fatalf("OpenAPISpec() missing %s", route)
	}
	if got, want := byMethod["post"], "investigateHardcodedSecrets"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
}

// openAPIOperationIDs walks the assembled spec and returns every operationId
// keyed by path then HTTP method. It reads the spec the handlers actually
// serve, so a path file that stops being assembled shows up as a missing
// entry rather than a silently passing assertion.
func openAPIOperationIDs(t *testing.T) map[string]map[string]string {
	t.Helper()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPISpec() JSON error = %v, want nil", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPISpec() has no paths object")
	}

	out := make(map[string]map[string]string, len(paths))
	for path, raw := range paths {
		operations, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOp := range operations {
			operation, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			id, ok := operation["operationId"].(string)
			if !ok {
				continue
			}
			if out[path] == nil {
				out[path] = map[string]string{}
			}
			out[path][method] = id
		}
	}
	return out
}
