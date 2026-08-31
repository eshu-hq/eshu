// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	containerimagetools "github.com/eshu-hq/eshu/go/internal/mcp/containerimage"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// containerImageRouteTools lists every tool the child package owns.
var containerImageRouteTools = []string{
	"list_container_image_identities",
	"list_container_image_tag_history",
	"count_container_image_identities",
	"get_container_image_identity_inventory",
}

// containerImageDispatchPaths pins each tool's path as dispatch selects it.
// Tag history is the asymmetric one: the other three sit under
// /api/v0/supply-chain/container-images/identities, while tag history is
// mounted at /api/v0/images/tag-history.
var containerImageDispatchPaths = map[string]string{
	"list_container_image_identities":        "/api/v0/supply-chain/container-images/identities",
	"list_container_image_tag_history":       "/api/v0/images/tag-history",
	"count_container_image_identities":       "/api/v0/supply-chain/container-images/identities/count",
	"get_container_image_identity_inventory": "/api/v0/supply-chain/container-images/identities/inventory",
}

// containerImageDispatchQueryKeys is the exact key set each request must still
// carry through dispatch, where the handler reads it.
var containerImageDispatchQueryKeys = map[string][]string{
	"list_container_image_identities": {
		"after_identity_id",
		"digest",
		"image_ref",
		"limit",
		"outcome",
		"repository_id",
		"source_repository_id",
	},
	"list_container_image_tag_history": {
		"limit",
		"offset",
		"repository_id",
		"tag",
	},
	"count_container_image_identities": {
		"digest",
		"image_ref",
		"outcome",
		"repository_id",
		"source_repository_id",
	},
	"get_container_image_identity_inventory": {
		"digest",
		"group_by",
		"image_ref",
		"limit",
		"offset",
		"outcome",
		"repository_id",
		"source_repository_id",
	},
}

func TestResolveRouteUsesExactContainerImageChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_identity_id":    "container-image-identity-1",
			"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
			"group_by":             "identity_strength",
			"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
			"limit":                float64(25),
			"offset":               float64(7),
			"outcome":              "exact_digest",
			"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
			"source_repository_id": "github.com/eshu-hq/eshu",
			"tag":                  "1.2.3",
		}},
		{name: "digest only", args: map[string]any{"digest": "sha256:abc"}},
		{name: "malformed", args: map[string]any{
			"digest":        42,
			"group_by":      []string{"outcome"},
			"limit":         "25",
			"offset":        true,
			"outcome":       nil,
			"repository_id": struct{}{},
			"tag":           false,
		}},
	}

	for _, tool := range containerImageRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := containerimagetools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
			// This comparison proves the adapter transcribes method, path,
			// body, and query faithfully. It cannot prove the child selected
			// the right values -- both sides come from the same selector -- so
			// the literal path and key assertions below carry that claim.
			want := &route{
				method: request.Method,
				path:   request.Path,
				body:   request.Body,
				query:  request.Query,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolveRoute(%s, %s) = %#v, want child request %#v", tool, tt.name, got, want)
			}
		}
	}
}

// TestContainerImageDispatchKeepsEveryQueryKey proves each key survives the
// adapter boundary against literal expectations rather than against the child
// selector. The failure shapes differ by tool: the identity listing 400s
// without limit and without a scope anchor, tag history 400s without
// repository_id and tag, and a dropped aggregate filter returns 200 over a
// wider scope than the caller asked for.
func TestContainerImageDispatchKeepsEveryQueryKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"after_identity_id":    "container-image-identity-1",
		"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"group_by":             "identity_strength",
		"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
		"limit":                float64(25),
		"offset":               float64(7),
		"outcome":              "exact_digest",
		"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
		"source_repository_id": "github.com/eshu-hq/eshu",
		"tag":                  "1.2.3",
	}
	want := map[string]string{
		"after_identity_id":    "container-image-identity-1",
		"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"group_by":             "identity_strength",
		"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
		"limit":                "25",
		"offset":               "7",
		"outcome":              "exact_digest",
		"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
		"source_repository_id": "github.com/eshu-hq/eshu",
		"tag":                  "1.2.3",
	}

	for _, tool := range containerImageRouteTools {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.method != "GET" {
			t.Errorf("%s: method = %q, want GET", tool, got.method)
		}
		if wantPath := containerImageDispatchPaths[tool]; got.path != wantPath {
			t.Errorf("%s: path = %q, want %q", tool, got.path, wantPath)
		}
		if got.body != nil {
			t.Errorf("%s: body = %#v, want nil", tool, got.body)
		}
		keys := containerImageDispatchQueryKeys[tool]
		if n, wantN := len(got.query), len(keys); n != wantN {
			t.Fatalf("%s: query carries %d keys (%#v), want %d", tool, n, got.query, wantN)
		}
		for _, key := range keys {
			value, present := got.query[key]
			if !present {
				t.Errorf("%s: dispatch dropped %q entirely", tool, key)
				continue
			}
			if value != want[key] {
				t.Errorf("%s: query[%s] = %q, want %q", tool, key, value, want[key])
			}
		}
	}

	// Paging defaults are not uniform across the family, and only the
	// inventory falls back to a grouping dimension.
	for _, tt := range []struct {
		tool       string
		wantLimit  string
		wantOffset string
		wantGroup  string
	}{
		{tool: "list_container_image_identities", wantLimit: "50"},
		{tool: "list_container_image_tag_history", wantLimit: "50", wantOffset: "0"},
		{tool: "count_container_image_identities"},
		{tool: "get_container_image_identity_inventory", wantLimit: "100", wantOffset: "0", wantGroup: "outcome"},
	} {
		bare, err := resolveRoute(tt.tool, map[string]any{})
		if err != nil {
			t.Fatalf("resolveRoute(%s, empty) error = %v, want nil", tt.tool, err)
		}
		if got := bare.query["limit"]; got != tt.wantLimit {
			t.Errorf("%s: absent limit -> %q, want %q", tt.tool, got, tt.wantLimit)
		}
		if got := bare.query["offset"]; got != tt.wantOffset {
			t.Errorf("%s: absent offset -> %q, want %q", tt.tool, got, tt.wantOffset)
		}
		if got := bare.query["group_by"]; got != tt.wantGroup {
			t.Errorf("%s: absent group_by -> %q, want %q", tt.tool, got, tt.wantGroup)
		}
	}
}

// TestContainerImageTagHistoryKeepsItsOwnPathPrefixThroughDispatch guards the
// one path in this family that does not share the others' prefix. Three tools
// resolve under /api/v0/supply-chain/container-images/identities; tag history
// resolves to /api/v0/images/tag-history, which is where
// query.TagHistoryHandler.Mount registers it. Normalizing it onto the sibling
// prefix would select a path the query mux does not serve.
func TestContainerImageTagHistoryKeepsItsOwnPathPrefixThroughDispatch(t *testing.T) {
	t.Parallel()

	got, err := resolveRoute("list_container_image_tag_history", map[string]any{
		"repository_id": "oci-registry://ghcr.io/eshu-hq/api",
		"tag":           "1.2.3",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got.path != "/api/v0/images/tag-history" {
		t.Fatalf("tag history path = %q, want /api/v0/images/tag-history", got.path)
	}
	for _, tool := range []string{
		"list_container_image_identities",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
	} {
		sibling, err := resolveRoute(tool, map[string]any{})
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if sibling.path == got.path {
			t.Fatalf("%s resolved to the tag-history path %q", tool, got.path)
		}
	}
}

// TestRepositoryRouteStillOwnsItsArmsAfterContainerImage proves the sixth
// delegation added in front of the repository switch claims only this family.
// Two of this family's builders came out of dispatch_supply_chain.go, so the
// six supply-chain tools whose builders stayed there are listed explicitly
// alongside the neighbouring arms and the earlier extracted families.
func TestRepositoryRouteStillOwnsItsArmsAfterContainerImage(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"get_package_registry_package_inventory",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"get_ci_cd_run_correlation_inventory",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"count_secrets_iam_posture",
		"list_observability_coverage_correlations",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_supply_chain_impact_findings",
		"explain_supply_chain_impact",
		"list_security_alert_reconciliations",
		"list_sbom_attestation_attachments",
		"get_repository_stats",
	} {
		if _, handled := containerImageRoute(tool, map[string]any{}); handled {
			t.Errorf("containerImageRoute(%s) handled = true, want false", tool)
		}
		got, ok, err := repositoryRoute(tool, map[string]any{})
		if err != nil {
			t.Errorf("repositoryRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if !ok || got == nil {
			t.Errorf("repositoryRoute(%s) ok = %v, route = %v, want a route", tool, ok, got)
		}
	}

	// An unknown tool still falls through the repository switch untouched.
	if got, ok, err := repositoryRoute("not_a_tool", map[string]any{}); ok || got != nil || err != nil {
		t.Fatalf("repositoryRoute(not_a_tool) = (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestContainerImageRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the four owned names are claimed, and
// near-miss names are not.
func TestContainerImageRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range containerImageRouteTools {
		if _, handled := containerImageRoute(tool, map[string]any{}); !handled {
			t.Errorf("containerImageRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"list_container_image_identity",
		"list_container_image_identities_extra",
		"count_container_image_identity",
		"count_container_image_tag_history",
		"get_container_image_identity_inventories",
		"get_container_image_tag_history",
		"list_container_image_tag_histories",
		"container_image_identities",
		"LIST_CONTAINER_IMAGE_IDENTITIES",
		"not_a_tool",
	} {
		if _, handled := containerImageRoute(tool, map[string]any{}); handled {
			t.Errorf("containerImageRoute(%q) handled = true, want false", tool)
		}
	}
}
