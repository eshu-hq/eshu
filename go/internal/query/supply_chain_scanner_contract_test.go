// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVulnerabilityScannerReadContractIdentifiesFilters(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/vulnerability-scanner/contract",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if envelope.Truth.Capability != vulnerabilityScannerReadContractCapability {
		t.Fatalf("truth.capability = %q, want %q", envelope.Truth.Capability, vulnerabilityScannerReadContractCapability)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", envelope.Data)
	}
	filters, ok := data["filters"].([]any)
	if !ok {
		t.Fatalf("filters = %T, want []any", data["filters"])
	}
	got := map[string]map[string]any{}
	for _, raw := range filters {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("filter row = %T, want map[string]any", raw)
		}
		name, _ := row["name"].(string)
		got[name] = row
	}
	wantFilters := []string{
		"repository", "package", "advisory", "image_digest",
		"workload", "service", "environment", "ecosystem", "language",
		"severity", "status", "readiness", "provider_state",
	}
	for _, name := range wantFilters {
		if got[name] == nil {
			t.Fatalf("contract missing filter %q; filters = %#v", name, got)
		}
	}
	if semantics := strings.Join(scannerContractStringSlice(got["provider_state"]["semantics"]), ","); !strings.Contains(semantics, "provider-only") {
		t.Fatalf("provider_state semantics = %q, want provider-only", semantics)
	}
	if support := got["language"]["support"]; support != "unsupported" {
		t.Fatalf("language support = %#v, want unsupported", support)
	}
	if support := got["readiness"]["support"]; support != "missing-evidence driven" {
		t.Fatalf("readiness support = %#v, want missing-evidence driven", support)
	}
	if backing := got["repository"]["backing"]; !strings.Contains(backing.(string), "reducer") {
		t.Fatalf("repository backing = %#v, want reducer read model", backing)
	}

	routes := scannerContractObjectListByName(t, data["routes"], "routes")
	impactExplain := routes["impact_explain"]
	if impactExplain == nil {
		t.Fatalf("contract missing impact_explain route; routes = %#v", routes)
	}
	ordering, _ := impactExplain["ordering"].(string)
	if !strings.Contains(ordering, "outcome=ambiguous_scope") {
		t.Fatalf("impact_explain ordering = %q, want ambiguous_scope refusal envelope", ordering)
	}
	if strings.Contains(ordering, "return conflict") {
		t.Fatalf("impact_explain ordering = %q, want no stale conflict contract", ordering)
	}
}

func TestVulnerabilityScannerReadContractDefinesRemediationPacket(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/vulnerability-scanner/contract",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", envelope.Data)
	}
	packet, ok := data["remediation_packet"].(map[string]any)
	if !ok {
		t.Fatalf("remediation_packet = %T, want object", data["remediation_packet"])
	}
	if got := packet["schema_version"]; got != "eshu.supply_chain_remediation_packet.v1" {
		t.Fatalf("remediation_packet.schema_version = %#v, want eshu.supply_chain_remediation_packet.v1", got)
	}

	sections := scannerContractObjectListByName(t, packet["sections"], "sections")
	for _, name := range []string{
		"vulnerability_fact",
		"package_fact",
		"sbom_subject",
		"image_digest",
		"workload",
		"service",
		"owner",
		"exposure",
		"remediation_recommendation",
	} {
		if sections[name] == nil {
			t.Fatalf("remediation packet missing section %q; sections = %#v", name, sections)
		}
	}
	recommendation := sections["remediation_recommendation"]
	if evidence := scannerContractStringSlice(recommendation["deterministic_evidence"]); len(evidence) == 0 {
		t.Fatalf("remediation recommendation deterministic_evidence = %#v, want at least one deterministic citation field", recommendation["deterministic_evidence"])
	}
	if optionalSemantic, ok := recommendation["optional_semantic_required"].(bool); !ok || optionalSemantic {
		t.Fatalf("remediation recommendation optional_semantic_required = %#v, want false", recommendation["optional_semantic_required"])
	}

	missingStates := scannerContractObjectListByName(t, packet["missing_states"], "missing_states")
	for _, name := range []string{"missing_owner", "missing_workload", "stale_image", "permission_hidden"} {
		if missingStates[name] == nil {
			t.Fatalf("remediation packet missing state %q; states = %#v", name, missingStates)
		}
	}

	surfaces := scannerContractObjectListByName(t, packet["surfaces"], "surfaces")
	for _, name := range []string{"api", "mcp", "console"} {
		if surfaces[name] == nil {
			t.Fatalf("remediation packet missing surface %q; surfaces = %#v", name, surfaces)
		}
	}
	if got := surfaces["api"]["representation"]; got != "/api/v0/supply-chain/impact/explain response payload" {
		t.Fatalf("api representation = %#v, want explain payload", got)
	}
	if got := surfaces["mcp"]["representation"]; got != "explain_supply_chain_impact envelope resource" {
		t.Fatalf("mcp representation = %#v, want MCP envelope resource", got)
	}

	securityReview, ok := packet["security_review"].(map[string]any)
	if !ok {
		t.Fatalf("security_review = %T, want object", packet["security_review"])
	}
	if controls := scannerContractStringSlice(securityReview["false_positive_controls"]); len(controls) == 0 {
		t.Fatalf("security_review.false_positive_controls = %#v, want controls", securityReview["false_positive_controls"])
	}
	if controls := scannerContractStringSlice(securityReview["leakage_controls"]); len(controls) == 0 {
		t.Fatalf("security_review.leakage_controls = %#v, want controls", securityReview["leakage_controls"])
	}
}

func TestVulnerabilityScannerReadContractRejectsUnknownRouteNames(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/vulnerability-scanner/contract?route=whole_graph",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "route must be one of") {
		t.Fatalf("body = %q, want route guidance", w.Body.String())
	}
}

func scannerContractStringSlice(raw any) []string {
	items, _ := raw.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func scannerContractObjectListByName(t *testing.T, raw any, field string) map[string]map[string]any {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", field, raw)
	}
	out := make(map[string]map[string]any, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s row = %T, want map[string]any", field, item)
		}
		name, _ := row["name"].(string)
		if name == "" {
			t.Fatalf("%s row missing name: %#v", field, row)
		}
		out[name] = row
	}
	return out
}
