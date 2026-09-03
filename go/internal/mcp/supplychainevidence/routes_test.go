// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainevidencetools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// pagedListingTools are the two routes that page with a limit-carrying
// listing shape: list_advisory_evidence and list_sbom_attestation_attachments
// both default limit to 50. get_sbom_attestation_attachment_inventory also
// pages but defaults limit to 100 and adds offset and group_by, so it is
// exercised on its own below.
var pagedListingTools = []string{
	"list_advisory_evidence",
	"list_sbom_attestation_attachments",
}

// familyTools are all five names this package owns, in the order the root
// repository switch used to answer them.
var familyTools = []string{
	"list_advisory_evidence",
	"get_vulnerability_scanner_read_contract",
	"list_sbom_attestation_attachments",
	"count_sbom_attestation_attachments",
	"get_sbom_attestation_attachment_inventory",
}

// neighborTools are tool names answered by other families in the root
// repository switch or by other extracted children. This package must claim
// none of them.
var neighborTools = []string{
	"list_indexed_repositories",
	"count_repositories_by_language",
	"list_repositories_by_language",
	"get_repository_language_inventory",
	"get_repository_stats",
	"get_repo_context",
	"get_repo_story",
	"get_repo_summary",
	"get_repository_coverage",
	"get_repository_freshness",
	"get_relationship_evidence",
	"list_service_catalog_correlations",
	"list_admission_decisions",
	"list_package_registry_packages",
	"count_package_registry_packages",
	"list_ci_cd_run_correlations",
	"list_codeowners_ownership",
	"list_kubernetes_correlations",
	"list_observability_coverage_correlations",
	"list_container_image_identities",
	"list_secrets_iam_identity_trust_chains",
	"count_secrets_iam_posture",
	"list_supply_chain_impact_findings",
	"count_supply_chain_impact_findings",
	"get_supply_chain_impact_inventory",
	"explain_supply_chain_impact",
	"list_security_alert_reconciliations",
	"list_advisory_evidences",
	"list_advisory_evidenc",
	"get_vulnerability_scanner_read_contracts",
	"get_vulnerability_scanner_read_contrac",
	"list_sbom_attestation_attachment",
	"list_sbom_attestation_attachments_extra",
	"count_sbom_attestation_attachment",
	"count_sbom_attestation_attachments_extra",
	"get_sbom_attestation_attachment_inventories",
	"get_sbom_attestation_attachment_inventor",
	"LIST_ADVISORY_EVIDENCE",
	"GET_VULNERABILITY_SCANNER_READ_CONTRACT",
	"",
	"not_a_tool",
}

// populatedArguments carries every key any of the five routes reads, each
// with a distinct value, so a key swapped between two routes -- for example
// document_id and document_digest between the SBOM listing, count, and
// inventory routes -- fails the exact request comparisons below rather than
// passing on a shared value.
var populatedArguments = routecontract.Arguments{
	"advisory_id":         "GHSA-aaaa-bbbb-cccc",
	"after_advisory_key":  "CVE-2026-0001",
	"after_attachment_id": "attachment-cursor",
	"artifact_kind":       "sbom",
	"attachment_status":   "verified",
	"cve_id":              "CVE-2026-0002",
	"digest":              "sha256:digest-1",
	"document_digest":     "sha256:doc-digest-1",
	"document_id":         "doc-1",
	"group_by":            "artifact_kind",
	"limit":               float64(25),
	"offset":              float64(5),
	"package_id":          "pkg:npm/example",
	"repository_id":       "repo://example/api",
	"route":               "impact_findings",
	"service_id":          "service:payments-api",
	"source":              "osv",
	"subject_digest":      "sha256:subject-1",
	"unused_decoy":        "ignored",
	"workload_id":         "workload:payments-api",
}

func TestRouteOwnsExactlyTheSupplyChainEvidenceFamily(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", toolName)
			continue
		}
		if request.Method != "GET" {
			t.Errorf("Route(%s) method = %q, want GET", toolName, request.Method)
		}
		if request.Body != nil {
			t.Errorf("Route(%s) body = %#v, want nil", toolName, request.Body)
		}
	}

	for _, toolName := range neighborTools {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesSupplyChainEvidenceRequestContracts(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		toolName string
		want     routecontract.Request
	}{
		{
			toolName: "get_vulnerability_scanner_read_contract",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/vulnerability-scanner/contract", Query: map[string]string{
				"route": "impact_findings",
			}},
		},
		{
			toolName: "list_advisory_evidence",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/advisories/evidence", Query: map[string]string{
				"advisory_id":        "GHSA-aaaa-bbbb-cccc",
				"after_advisory_key": "CVE-2026-0001",
				"cve_id":             "CVE-2026-0002",
				"limit":              "25",
				"package_id":         "pkg:npm/example",
				"repository_id":      "repo://example/api",
				"service_id":         "service:payments-api",
				"source":             "osv",
				"workload_id":        "workload:payments-api",
			}},
		},
		{
			toolName: "list_sbom_attestation_attachments",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments", Query: map[string]string{
				"after_attachment_id": "attachment-cursor",
				"artifact_kind":       "sbom",
				"attachment_status":   "verified",
				"digest":              "sha256:digest-1",
				"document_digest":     "sha256:doc-digest-1",
				"document_id":         "doc-1",
				"limit":               "25",
				"repository_id":       "repo://example/api",
				"service_id":          "service:payments-api",
				"subject_digest":      "sha256:subject-1",
				"workload_id":         "workload:payments-api",
			}},
		},
		{
			toolName: "count_sbom_attestation_attachments",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments/count", Query: map[string]string{
				"subject_digest":    "sha256:subject-1",
				"document_id":       "doc-1",
				"document_digest":   "sha256:doc-digest-1",
				"attachment_status": "verified",
				"artifact_kind":     "sbom",
				"repository_id":     "repo://example/api",
				"workload_id":       "workload:payments-api",
				"service_id":        "service:payments-api",
			}},
		},
		{
			toolName: "get_sbom_attestation_attachment_inventory",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments/inventory", Query: map[string]string{
				"group_by":          "artifact_kind",
				"subject_digest":    "sha256:subject-1",
				"document_id":       "doc-1",
				"document_digest":   "sha256:doc-digest-1",
				"attachment_status": "verified",
				"artifact_kind":     "sbom",
				"repository_id":     "repo://example/api",
				"workload_id":       "workload:payments-api",
				"service_id":        "service:payments-api",
				"limit":             "25",
				"offset":            "5",
			}},
		},
	} {
		request, handled := Route(tt.toolName, populatedArguments)
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", tt.toolName)
			continue
		}
		if !reflect.DeepEqual(request, tt.want) {
			t.Errorf("Route(%s) = %#v, want %#v", tt.toolName, request, tt.want)
		}
	}
}

// TestRouteDefaultsGroupByOnInventoryOnly pins the one branch in this family:
// get_sbom_attestation_attachment_inventory substitutes "attachment_status"
// for an absent or empty group_by. No other route in the family reads
// group_by at all, so an accidental default leaking onto a sibling route must
// fail here.
func TestRouteDefaultsGroupByOnInventoryOnly(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		groupBy any
	}{
		{name: "absent", groupBy: nil},
		{name: "empty string", groupBy: ""},
	} {
		args := routecontract.Arguments{"subject_digest": "sha256:subject-1"}
		if tt.groupBy != nil {
			args["group_by"] = tt.groupBy
		}
		request, handled := Route("get_sbom_attestation_attachment_inventory", args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		if got, want := request.Query["group_by"], "attachment_status"; got != want {
			t.Errorf("%s: group_by = %q, want default %q", tt.name, got, want)
		}
	}

	// An explicit group_by is forwarded unchanged, not overridden.
	request, handled := Route("get_sbom_attestation_attachment_inventory", routecontract.Arguments{"group_by": "artifact_kind"})
	if !handled {
		t.Fatal("Route handled = false, want true")
	}
	if got, want := request.Query["group_by"], "artifact_kind"; got != want {
		t.Errorf("explicit group_by = %q, want %q", got, want)
	}

	// No other route in the family carries a group_by key at all.
	for _, toolName := range []string{
		"get_vulnerability_scanner_read_contract",
		"list_advisory_evidence",
		"list_sbom_attestation_attachments",
		"count_sbom_attestation_attachments",
	} {
		request, handled := Route(toolName, routecontract.Arguments{"group_by": "artifact_kind"})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		if value, present := request.Query["group_by"]; present {
			t.Errorf("Route(%s) carries group_by = %q, want the key absent", toolName, value)
		}
	}
}

// TestRouteKeepsTheAttachmentCountUnpaged pins the family's other asymmetry:
// count_sbom_attestation_attachments carries the same filter keys as the
// listing and inventory routes but never limit, offset, or group_by. A count
// has no page to size and nothing to seek past, so a mutant that adds paging
// keys here must fail.
func TestRouteKeepsTheAttachmentCountUnpaged(t *testing.T) {
	t.Parallel()

	request, handled := Route("count_sbom_attestation_attachments", populatedArguments)
	if !handled {
		t.Fatal("Route(count_sbom_attestation_attachments) handled = false, want true")
	}
	if got, want := len(request.Query), 8; got != want {
		t.Fatalf("count carries %d query keys (%#v), want %d", got, request.Query, want)
	}
	for _, key := range []string{"limit", "offset", "group_by", "after_attachment_id", "digest"} {
		if value, present := request.Query[key]; present {
			t.Errorf("count carries %q = %q, want the key absent", key, value)
		}
	}
}

func TestRouteAppliesSupplyChainEvidenceDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	// The two 50-default listings fall back to the dispatcher's documented
	// limit default of 50 when absent.
	for _, toolName := range pagedListingTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		if got := request.Query["limit"]; got != "50" {
			t.Errorf("Route(%s) absent limit -> %q, want 50", toolName, got)
		}
	}

	// The inventory route defaults limit to 100 and offset to 0.
	request, handled := Route("get_sbom_attestation_attachment_inventory", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(get_sbom_attestation_attachment_inventory) handled = false, want true")
	}
	if got := request.Query["limit"]; got != "100" {
		t.Errorf("inventory absent limit -> %q, want 100", got)
	}
	if got := request.Query["offset"]; got != "0" {
		t.Errorf("inventory absent offset -> %q, want 0", got)
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly on every
	// limit-carrying route, including float truncation toward zero and the
	// fallback for unsupported types.
	for _, toolName := range append(append([]string{}, pagedListingTools...), "get_sbom_attestation_attachment_inventory") {
		def := "50"
		if toolName == "get_sbom_attestation_attachment_inventory" {
			def = "100"
		}
		for _, tt := range []struct {
			limit any
			want  string
		}{
			{limit: 25, want: "25"},
			{limit: int64(26), want: "26"},
			{limit: 27.9, want: "27"},
			{limit: -3.9, want: "-3"},
			{limit: -7, want: "-7"},
			{limit: 0, want: "0"},
			{limit: "25", want: def},
			{limit: true, want: def},
			{limit: nil, want: def},
			{limit: float32(25), want: def},
		} {
			request, _ := Route(toolName, routecontract.Arguments{"limit": tt.limit})
			if got := request.Query["limit"]; got != tt.want {
				t.Errorf("Route(%s) limit=%#v -> %q, want %q", toolName, tt.limit, got, tt.want)
			}
		}
	}

	// Wrong-typed and absent string arguments both read as empty, never as a
	// formatted Go value.
	for _, tt := range []struct {
		toolName string
		key      string
		value    any
	}{
		{toolName: "get_vulnerability_scanner_read_contract", key: "route", value: 42},
		{toolName: "list_advisory_evidence", key: "advisory_id", value: nil},
		{toolName: "list_sbom_attestation_attachments", key: "digest", value: []string{"sha256:x"}},
		{toolName: "count_sbom_attestation_attachments", key: "artifact_kind", value: struct{}{}},
		{toolName: "get_sbom_attestation_attachment_inventory", key: "group_by", value: true},
	} {
		request, handled := Route(tt.toolName, routecontract.Arguments{tt.key: tt.value})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.toolName)
		}
		want := ""
		if tt.toolName == "get_sbom_attestation_attachment_inventory" && tt.key == "group_by" {
			// group_by substitutes its own default for a non-string value.
			want = "attachment_status"
		}
		if got := request.Query[tt.key]; got != want {
			t.Errorf("Route(%s) %s=%#v -> %q, want %q", tt.toolName, tt.key, tt.value, got, want)
		}
	}
}

func TestRouteHandlesNilAndTypedNilSupplyChainEvidenceArguments(t *testing.T) {
	t.Parallel()

	empty := map[string]routecontract.Request{
		"get_vulnerability_scanner_read_contract": {Method: "GET", Path: "/api/v0/supply-chain/vulnerability-scanner/contract", Query: map[string]string{
			"route": "",
		}},
		"list_advisory_evidence": {Method: "GET", Path: "/api/v0/supply-chain/advisories/evidence", Query: map[string]string{
			"advisory_id":        "",
			"after_advisory_key": "",
			"cve_id":             "",
			"limit":              "50",
			"package_id":         "",
			"repository_id":      "",
			"service_id":         "",
			"source":             "",
			"workload_id":        "",
		}},
		"list_sbom_attestation_attachments": {Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments", Query: map[string]string{
			"after_attachment_id": "",
			"artifact_kind":       "",
			"attachment_status":   "",
			"digest":              "",
			"document_digest":     "",
			"document_id":         "",
			"limit":               "50",
			"repository_id":       "",
			"service_id":          "",
			"subject_digest":      "",
			"workload_id":         "",
		}},
		"count_sbom_attestation_attachments": {Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments/count", Query: map[string]string{
			"subject_digest":    "",
			"document_id":       "",
			"document_digest":   "",
			"attachment_status": "",
			"artifact_kind":     "",
			"repository_id":     "",
			"workload_id":       "",
			"service_id":        "",
		}},
		"get_sbom_attestation_attachment_inventory": {Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments/inventory", Query: map[string]string{
			"group_by":          "attachment_status",
			"subject_digest":    "",
			"document_id":       "",
			"document_digest":   "",
			"attachment_status": "",
			"artifact_kind":     "",
			"repository_id":     "",
			"workload_id":       "",
			"service_id":        "",
			"limit":             "100",
			"offset":            "0",
		}},
	}

	var typedNil map[string]any
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		for _, toolName := range familyTools {
			request, handled := Route(toolName, tt.args)
			if !handled {
				t.Fatalf("%s: Route(%s) handled = false, want true", tt.name, toolName)
			}
			if !reflect.DeepEqual(request, empty[toolName]) {
				t.Fatalf("%s: Route(%s) = %#v, want %#v", tt.name, toolName, request, empty[toolName])
			}
		}
	}
}

// aliasProbeKeys names, per tool, one query key that route actually carries,
// so the mutation below is observable. get_vulnerability_scanner_read_contract
// carries no repository_id -- it reads only route -- so it needs its own key.
var aliasProbeKeys = map[string]string{
	"list_advisory_evidence":                    "repository_id",
	"get_vulnerability_scanner_read_contract":   "route",
	"list_sbom_attestation_attachments":         "repository_id",
	"count_sbom_attestation_attachments":        "repository_id",
	"get_sbom_attestation_attachment_inventory": "repository_id",
}

func TestRouteDoesNotAliasCallerSupplyChainEvidenceArguments(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		probeKey := aliasProbeKeys[toolName]
		probeValue := "repo://example/api"
		if probeKey == "route" {
			probeValue = "impact_findings"
		}
		args := routecontract.Arguments{probeKey: probeValue, "limit": float64(25)}
		request, handled := Route(toolName, args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		request.Query[probeKey] = "mutated"
		if got := args[probeKey]; got != probeValue {
			t.Fatalf("Route(%s) mutated caller arguments through the returned query: %s = %#v", toolName, probeKey, got)
		}
		if len(args) != 2 {
			t.Fatalf("Route(%s) grew caller arguments to %d keys, want 2", toolName, len(args))
		}

		// Two calls with the same arguments hand back independent query maps.
		first, _ := Route(toolName, args)
		second, _ := Route(toolName, args)
		first.Query[probeKey] = "mutated"
		if got := second.Query[probeKey]; got != probeValue {
			t.Fatalf("Route(%s) shares a query map between calls: %s = %q", toolName, probeKey, got)
		}
	}
}
