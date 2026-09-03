// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestGrantBoundRoutesDeclareForbiddenResponse holds the published contract to
// what the middleware actually does to an all-scope caller.
//
// scopedBearerRouteDenialReason (auth.go) refuses a tenant-bound all-scope
// bearer with 403 on EVERY route the ledger classes scopedRouteGrantBound
// under hosted_multi_tenant or an unrecognized governance mode, and the cookie
// branch has been able to return the same 403 since #6457. The OpenAPI
// document said so on the freshness operations alone: GET
// /api/v0/repositories, to take the route the codex PR #6497 review named,
// declared 200/500/503/504 and no Forbidden, so a generated client had no
// case for a status the server returns on a routine deployment posture.
//
// The gate is deliberately class-driven rather than a hand-typed list: the
// next route added to the ledger as grant-bound fails here until its operation
// declares the 403, the same way TestScopedTokenAllowlistCompleteness makes a
// new scoped route declare its marker. Identity-bound, tenant-data-free,
// deployment-scoped, and transitive routes are exempt because the policy check
// never refuses them; several declare a 403 anyway for their own
// authorization reasons, which this test neither requires nor forbids.
func TestGrantBoundRoutesDeclareForbiddenResponse(t *testing.T) {
	t.Parallel()

	declared := openAPIOperationResponseCodes(t)

	var missing, unmapped []string
	for name, class := range scopedTokenAdvertisedRoutes {
		if class != scopedRouteGrantBound {
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
		t.Errorf("grant-bound ledger routes with no OpenAPI operation (%d):\n  %s",
			len(unmapped), strings.Join(unmapped, "\n  "))
	}
	if len(missing) > 0 {
		t.Errorf("grant-bound routes whose OpenAPI operation declares no 403 (%d) -- an all-scope caller is refused here under hosted_multi_tenant, so the operation must declare \"403\": {\"$ref\": \"#/components/responses/Forbidden\"}:\n  %s",
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
