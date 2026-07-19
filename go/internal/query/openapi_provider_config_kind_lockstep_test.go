// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestProviderConfigKindOpenAPIEnumsStayInLockstep is the guard that would
// have caught the F-5 (issue #5166) gap: the admin write path
// (buildProviderConfigWrite) started accepting provider_kind "github", but
// the AdminProviderConfigWriteRequest / AdminProviderConfig OpenAPI
// component enums still listed only oidc/saml. scripts/verify-openapi.sh
// only checks that every mounted ROUTE has a path entry — it never inspects
// component-schema enum completeness — so a stale enum passed that gate
// silently.
//
// This test ties the two OpenAPI enums to the set of kinds the write path
// actually accepts:
//
//   - Every kind the write path accepts (writeKindGroups below) MUST appear
//     in AdminProviderConfigWriteRequest.provider_kind's enum, and the enum
//     MUST contain nothing else.
//   - Its stored external_<kind> form MUST appear in
//     AdminProviderConfig.provider_kind's enum (the read view), and that enum
//     MUST contain nothing else.
//   - buildProviderConfigWrite MUST actually accept each listed kind (get
//     past the kind switch) and reject an unknown one — so the canonical
//     list here cannot silently claim a kind the code rejects, or omit one it
//     accepts, without a rebuild turning something red.
//
// Mutation checks (all turn this test red):
//   - drop "github" from AdminProviderConfigWriteRequest's enum → the
//     "write enum missing github" assertion fires.
//   - drop "external_github" from AdminProviderConfig's enum → the read-enum
//     assertion fires.
//   - remove the buildGitHubProviderConfigWrite case from
//     buildProviderConfigWrite (so "github" 400s as unknown) → the
//     "write path must accept github" assertion fires.
func TestProviderConfigKindOpenAPIEnumsStayInLockstep(t *testing.T) {
	t.Parallel()

	// writeKindGroups is the canonical set of provider_kind values the admin
	// write path (go/internal/query/admin_provider_config_build.go's
	// buildProviderConfigWrite switch) accepts. Add a new kind here in the
	// same change that adds its buildXProviderConfigWrite case — the
	// acceptance sub-check below proves the code and this list agree.
	writeKindGroups := []string{"oidc", "saml", "github"}

	// Every accepted write kind must be accepted by the actual build switch,
	// and an unmistakably-bogus kind must be rejected — this keeps the
	// canonical list honest against the code (not just against itself).
	const unknownKindMarker = "provider_kind must be"
	for _, kind := range writeKindGroups {
		_, err := buildProviderConfigWrite(adminProviderConfigWriteRequest{ProviderKind: kind})
		// An empty body fails kind-specific field validation (missing
		// client_id, etc.) — that is fine; the point is it must NOT be the
		// unknown-kind rejection, i.e. the switch dispatched into a builder.
		if err != nil && strings.Contains(err.Error(), unknownKindMarker) {
			t.Fatalf("buildProviderConfigWrite rejected accepted kind %q as unknown: %v", kind, err)
		}
	}
	if _, err := buildProviderConfigWrite(adminProviderConfigWriteRequest{ProviderKind: "__never_a_real_kind__"}); err == nil ||
		!strings.Contains(err.Error(), unknownKindMarker) {
		t.Fatalf("buildProviderConfigWrite(bogus kind) error = %v, want an unknown-kind rejection containing %q", err, unknownKindMarker)
	}

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	schemas := mustMapField(t, mustMapField(t, spec, "components"), "schemas")

	// Write-request enum must equal the accepted-kind set exactly.
	writeReq := mustMapField(t, schemas, "AdminProviderConfigWriteRequest")
	writeProps := mustMapField(t, writeReq, "properties")
	writeEnum := enumStrings(t, mustMapField(t, writeProps, "provider_kind"), "AdminProviderConfigWriteRequest.provider_kind")
	assertSetEqual(t, "AdminProviderConfigWriteRequest.provider_kind enum", writeEnum, writeKindGroups)

	// The github field group's properties must be documented on the write
	// request (base_url/api_base_url are optional, allowed_orgs is the
	// required-non-empty github field — its presence as a documented property
	// is what this asserts; the required-ness is enforced server-side).
	for _, prop := range []string{"base_url", "api_base_url", "allowed_orgs"} {
		if _, ok := writeProps[prop]; !ok {
			t.Errorf("AdminProviderConfigWriteRequest.properties missing %q (github field group, issue #5166)", prop)
		}
	}

	// Read-view enum must equal the stored external_<kind> forms exactly.
	readView := mustMapField(t, schemas, "AdminProviderConfig")
	readEnum := enumStrings(t, mustMapField(t, mustMapField(t, readView, "properties"), "provider_kind"), "AdminProviderConfig.provider_kind")
	storedKinds := make([]string, 0, len(writeKindGroups))
	for _, kind := range writeKindGroups {
		storedKinds = append(storedKinds, "external_"+kind)
	}
	assertSetEqual(t, "AdminProviderConfig.provider_kind enum", readEnum, storedKinds)
}

func assertSetEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
		t.Errorf("%s = %v, want exactly %v", label, gotSorted, wantSorted)
	}
}
