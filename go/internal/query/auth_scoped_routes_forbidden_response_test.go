// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestPolicyGatedRoutesDeclareForbiddenResponse holds the published contract
// to what the middleware actually does to an all-scope caller.
//
// scopedBearerRouteDenialReason (auth.go) refuses a tenant-bound all-scope
// bearer with 403 under hosted_multi_tenant or an unrecognized governance
// mode, and the cookie branch has been able to return the same 403 since
// #6457. Neither branch asks whether the route is grant-bound specifically:
// both consult scopedRouteClass.admitsAllScopesSessionWithoutPolicy, which is
// true for the identity-bound and tenant-data-free populations alone. A
// deployment-scoped status read and the transitive POST /api/v0/ask are
// refused exactly like a grant-bound repository read, so this gate covers
// every class the predicate answers false for, not the grant-bound class
// alone. The earlier grant-bound-only condition passed while operations such
// as GET /api/v0/status/governance still omitted 403.
//
// The gate is deliberately predicate-driven rather than a hand-typed list or
// a hand-typed set of classes: a class added to scopedRouteClass, or an
// existing class moved to the refused side of the predicate, pulls its routes
// in here without a test edit, the same way TestScopedTokenAllowlistCompleteness
// makes a new scoped route declare its marker. Identity-bound and
// tenant-data-free routes are exempt because the policy check never refuses
// them; several declare a 403 anyway for their own authorization reasons,
// which this test neither requires nor forbids.
func TestPolicyGatedRoutesDeclareForbiddenResponse(t *testing.T) {
	t.Parallel()

	declared := openAPIOperationResponseCodes(t)

	var missing, unmapped []string
	for name, class := range scopedTokenAdvertisedRoutes {
		if class.admitsAllScopesSessionWithoutPolicy() {
			continue
		}
		codes, ok := declared[name]
		if !ok {
			unmapped = append(unmapped, name)
			continue
		}
		if _, ok := codes["403"]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unmapped)

	if len(unmapped) > 0 {
		t.Errorf("policy-gated ledger routes with no OpenAPI operation (%d):\n  %s",
			len(unmapped), strings.Join(unmapped, "\n  "))
	}
	if len(missing) > 0 {
		t.Errorf("policy-gated routes whose OpenAPI operation declares no 403 (%d) -- an all-scope caller is refused here under hosted_multi_tenant, so the operation must declare \"403\": {\"$ref\": \"#/components/responses/Forbidden\"}:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// openAPIOperationResponseCodes parses the served OpenAPI spec and returns, for
// every "METHOD /path" surface name, the set of response status codes its
// operation declares. It walks the assembled document rather than the
// openapi_paths_*.go sources so a route whose entry is dropped from the
// assembled spec cannot pass by having the string in an unreferenced constant.
func openAPIOperationResponseCodes(t *testing.T) map[string]map[string]struct{} {
	t.Helper()

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &doc); err != nil {
		t.Fatalf("parse OpenAPISpec(): %v", err)
	}
	codes := map[string]map[string]struct{}{}
	for path, item := range doc.Paths {
		for method, raw := range item {
			if _, ok := httpOperationMethodNames[strings.ToLower(method)]; !ok {
				continue
			}
			var operation struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}
			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatalf("parse operation %s %s: %v", method, path, err)
			}
			set := make(map[string]struct{}, len(operation.Responses))
			for code := range operation.Responses {
				set[code] = struct{}{}
			}
			codes[strings.ToUpper(method)+" "+path] = set
		}
	}
	return codes
}
