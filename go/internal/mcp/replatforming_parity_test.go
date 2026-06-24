// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Replatforming API/MCP parity + refusal-safety proof gate (issue #1968).
//
// Mode: in-process, fixture-backed Go proof. It drives the REAL query handlers
// and the REAL MCP dispatch path (dispatchTool -> resolveRoute -> mounted
// query.IaCHandler -> parseCanonicalEnvelope) against ONE handler instance, the
// same in-process parity approach the existing answer-parity tests use. A full
// remote-Compose run is the operator-facing complement documented in
// docs/public/reference/local-testing/remote-collector-e2e.md; this gate is the
// deterministic CI proof that the two surfaces cannot diverge.
//
// What the gate asserts, for plan, ownership-packets, and rollups:
//
//  1. API/MCP PARITY: for the same scope the HTTP route and the MCP tool return
//     byte-identical canonical envelope Data plus identical truth label
//     (level/basis/capability/freshness). Equality of the full Data block proves
//     bounded results AND source-state/readiness counts agree, not just smoke.
//  2. REFUSAL SAFETY: safety-gated findings (security_review_required, ambiguous,
//     stale, unknown) are surfaced as explicit refusals / rejected source state
//     with reasons and never silently omitted or promoted to ready.
//  3. PROFILE/TRUTH bounds: an unsupported profile returns unsupported_capability
//     on BOTH surfaces (never a downgraded answer), and the supported answer is
//     bounded, paginated, and truth-labeled.
//  4. NEGATIVE LEAKAGE: a credential-shaped raw tag value never appears in either
//     surface's serialized payload.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	replatformingPlanRoute      = "/api/v0/replatforming/plans"
	replatformingOwnershipRoute = "/api/v0/replatforming/ownership-packets"
	replatformingRollupsRoute   = "/api/v0/replatforming/rollups"
)

// requireReplatformingParity asserts the HTTP and MCP envelopes agree on the
// truth label and on the full Data block. Data equality is the strongest
// possible parity claim for these surfaces: it pins bounded results, source-state
// totals, readiness counts, refusal reasons, and stories to one contract.
func requireReplatformingParity(t *testing.T, httpEnv, mcpEnv *query.ResponseEnvelope) {
	t.Helper()

	httpCmp := extractComparable(t, httpEnv)
	mcpCmp := extractComparable(t, mcpEnv)
	requireParity(t, "http", "mcp", httpCmp, mcpCmp)

	httpData := canonicalJSON(t, httpEnv.Data)
	mcpData := canonicalJSON(t, mcpEnv.Data)
	if httpData != mcpData {
		t.Fatalf("replatforming Data parity mismatch:\n http=%s\n mcp =%s", httpData, mcpData)
	}
}

// canonicalJSON renders a value as canonical (key-sorted) JSON for deep equality.
func canonicalJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(out)
}

// requireNoSecretLeak proves the credential-shaped raw tag value never appears in
// a surface's full serialized payload (data, truth, and error).
func requireNoSecretLeak(t *testing.T, surface string, env *query.ResponseEnvelope) {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal %s envelope: %v", surface, err)
	}
	if strings.Contains(string(raw), replatformingSecretTagValue) {
		t.Fatalf("%s payload leaked raw secret tag value", surface)
	}
}

// dataMap reduces an envelope's Data to a map for field assertions.
func dataMap(t *testing.T, env *query.ResponseEnvelope) map[string]any {
	t.Helper()
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope Data = %T, want object", env.Data)
	}
	return data
}

// TestReplatformingRollupsAnswerParity proves the rollups surface agrees across
// HTTP and MCP and that per-item source states and readiness are preserved with
// refused items kept separate from import-ready ones.
func TestReplatformingRollupsAnswerParity(t *testing.T) {
	t.Parallel()

	handler := mountReplatformingHandler(t, query.ProfileLocalAuthoritative)
	httpEnv := httpEnvelope(t, handler, http.MethodPost, replatformingRollupsRoute, replatformingHTTPBody(nil))
	mcpEnv, summary := mcpEnvelope(t, handler, "get_replatforming_rollups", replatformingArgs())

	requireReplatformingParity(t, httpEnv, mcpEnv)
	requireNoSecretLeak(t, "http", httpEnv)
	requireNoSecretLeak(t, "mcp", mcpEnv)

	if got, want := httpEnv.Truth.Capability, "replatforming.rollups.readiness"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := httpEnv.Truth.Basis, query.TruthBasisSemanticFacts; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}

	data := dataMap(t, httpEnv)
	states := mapStringInt(t, data["source_state_totals"])
	// Refusal safety in the rollup: the read-only safety gate's rejected state
	// WINS over the evidence-derived state, so every safety-gated finding
	// (security_review_required, ambiguous, stale, unknown) resolves to rejected
	// and is never folded into a clean bucket. Of the six fixture rows, four are
	// safety-gated (rejected) and only the two safety-approved findings
	// (cloud_only Lambda and terraform_state_only table) are derived.
	assertCount(t, "source_state_totals.rejected", states["rejected"], 4)
	assertCount(t, "source_state_totals.derived", states["derived"], 2)
	// The masked evidence states must not silently re-appear as clean: the safety
	// gate folded them into rejected, never into ambiguous/stale/unknown buckets
	// that a consumer might read as a non-refused answer.
	for _, masked := range []string{"ambiguous", "stale", "unknown", "exact", "partial", "unavailable", "unsupported"} {
		assertCount(t, "source_state_totals."+masked, states[masked], 0)
	}
	// The full taxonomy is always present so an absent state never hides truth.
	for _, state := range query.AllReplatformingSourceStates() {
		if _, ok := states[string(state)]; !ok {
			t.Fatalf("source_state_totals missing taxonomy state %q", state)
		}
	}

	readiness := mapStringInt(t, data["readiness_totals"])
	// Only the safety-approved cloud_only Lambda is import-ready. The four
	// safety-gated findings (security-review, ambiguous, stale, unknown) are
	// refused, and the approved-but-not-importable terraform_state_only table
	// needs review. A refused finding must NEVER be counted import-ready.
	assertCount(t, "readiness_totals.import_ready", readiness["import_ready"], 1)
	assertCount(t, "readiness_totals.refused", readiness["refused"], 4)
	assertCount(t, "readiness_totals.needs_review", readiness["needs_review"], 1)
	if readiness["import_ready"]+readiness["needs_review"]+readiness["refused"] != 6 {
		t.Fatalf("readiness totals %v do not sum to the 6 bounded findings", readiness)
	}

	requireConvenienceSummary(t, summary, mcpEnv)
}

// TestReplatformingOwnershipAnswerParity proves the ownership-packet surface
// agrees across HTTP and MCP and that contested and refused findings are
// surfaced explicitly rather than promoted to a single fabricated owner.
func TestReplatformingOwnershipAnswerParity(t *testing.T) {
	t.Parallel()

	handler := mountReplatformingHandler(t, query.ProfileLocalAuthoritative)
	httpEnv := httpEnvelope(t, handler, http.MethodPost, replatformingOwnershipRoute, replatformingHTTPBody(nil))
	mcpEnv, summary := mcpEnvelope(t, handler, "find_unmanaged_resource_owners", replatformingArgs())

	requireReplatformingParity(t, httpEnv, mcpEnv)
	requireNoSecretLeak(t, "http", httpEnv)
	requireNoSecretLeak(t, "mcp", mcpEnv)

	if got, want := httpEnv.Truth.Capability, "replatforming.ownership.candidates"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}

	data := dataMap(t, httpEnv)
	assertCount(t, "packets_count", intVal(t, data["packets_count"]), 6)
	if intVal(t, data["rejected_count"]) < 1 {
		t.Fatalf("rejected_count = %d, want >= 1 (security-review finding must be rejected)", intVal(t, data["rejected_count"]))
	}
	if intVal(t, data["ambiguous_count"]) < 1 {
		t.Fatalf("ambiguous_count = %d, want >= 1 (contested ownership must be counted)", intVal(t, data["ambiguous_count"]))
	}

	// Refusal safety: the security-review packet must carry the rejected source
	// state, a security_review_required safety gate, and a refused import action.
	packets := sliceOfMaps(t, data["ownership_packets"])
	secretPacket := findPacketByItemID(t, packets, "fact:secret-store")
	if got := query.StringVal(secretPacket, "source_state"); got != "rejected" {
		t.Fatalf("secret packet source_state = %q, want rejected", got)
	}
	gate := mapVal(t, secretPacket["safety_gate"])
	if got := query.StringVal(gate, "outcome"); got != "security_review_required" {
		t.Fatalf("secret packet safety_gate.outcome = %q, want security_review_required", got)
	}
	if !boolVal(gate["review_required"]) {
		t.Fatalf("secret packet safety_gate.review_required = false, want true")
	}
	if !stringSliceContains(stringsOf(gate["refused_actions"]), "terraform_import_plan") {
		t.Fatalf("secret packet safety_gate.refused_actions = %v, want terraform_import_plan", gate["refused_actions"])
	}

	// The ambiguous packet must mark its contested candidates ambiguous, never
	// collapse them to one owner.
	ambiguousPacket := findPacketByItemID(t, packets, "fact:ambiguous-queue")
	if !ownershipPacketHasAmbiguousCandidate(ambiguousPacket) {
		t.Fatalf("ambiguous packet has no ambiguous candidate: %v", ambiguousPacket["owner_candidates"])
	}

	requireConvenienceSummary(t, summary, mcpEnv)
}

// TestReplatformingPlanAnswerParity proves the plan surface agrees across HTTP
// and MCP and that only the safety-approved cloud_only finding receives a ready
// import candidate while the rest are refused with reasons.
func TestReplatformingPlanAnswerParity(t *testing.T) {
	t.Parallel()

	handler := mountReplatformingHandler(t, query.ProfileLocalAuthoritative)
	httpEnv := httpEnvelope(t, handler, http.MethodPost, replatformingPlanRoute, replatformingHTTPBody(map[string]any{
		"scope_kind": "account",
	}))
	args := replatformingArgs()
	args["scope_kind"] = "account"
	mcpEnv, summary := mcpEnvelope(t, handler, "compose_replatforming_plan", args)

	requireReplatformingParity(t, httpEnv, mcpEnv)
	requireNoSecretLeak(t, "http", httpEnv)
	requireNoSecretLeak(t, "mcp", mcpEnv)

	if got, want := httpEnv.Truth.Capability, "replatforming.plan.readiness"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}

	data := dataMap(t, httpEnv)
	assertCount(t, "items_count", intVal(t, data["items_count"]), 6)
	assertCount(t, "ready_import_count", intVal(t, data["ready_import_count"]), 1)
	if intVal(t, data["refused_import_count"]) < 1 {
		t.Fatalf("refused_import_count = %d, want >= 1 (safety-gated findings must be refused)", intVal(t, data["refused_import_count"]))
	}

	// Refusal safety: the security-review item must carry a refused import
	// candidate with a security_review_required reason, never a ready one.
	plan := mapVal(t, data["plan"])
	items := sliceOfMaps(t, plan["items"])
	secretItem := findItemByStableID(t, items, "arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/payments/db")
	candidate := mapVal(t, secretItem["import_candidate"])
	if got := query.StringVal(candidate, "status"); got != "refused" {
		t.Fatalf("secret item import_candidate.status = %q, want refused", got)
	}
	if !stringSliceContains(stringsOf(candidate["refusal_reasons"]), "security_review_required") {
		t.Fatalf("secret item refusal_reasons = %v, want security_review_required", candidate["refusal_reasons"])
	}

	requireConvenienceSummary(t, summary, mcpEnv)
}

// TestReplatformingUnsupportedProfileParity proves every replatforming surface
// returns the same unsupported_capability error on both surfaces under a profile
// that cannot materialize the reducer-owned evidence, instead of a downgraded
// answer.
func TestReplatformingUnsupportedProfileParity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		route      string
		tool       string
		capability string
		extra      map[string]any
	}{
		{
			name:       "rollups",
			route:      replatformingRollupsRoute,
			tool:       "get_replatforming_rollups",
			capability: "replatforming.rollups.readiness",
		},
		{
			name:       "ownership",
			route:      replatformingOwnershipRoute,
			tool:       "find_unmanaged_resource_owners",
			capability: "replatforming.ownership.candidates",
		},
		{
			name:       "plan",
			route:      replatformingPlanRoute,
			tool:       "compose_replatforming_plan",
			capability: "replatforming.plan.readiness",
			extra:      map[string]any{"scope_kind": "account"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := mountReplatformingHandler(t, query.ProfileLocalLightweight)
			httpEnv := httpEnvelope(t, handler, http.MethodPost, tc.route, replatformingHTTPBody(tc.extra))
			args := replatformingArgs()
			for k, v := range tc.extra {
				args[k] = v
			}
			mcpEnv, summary := mcpEnvelope(t, handler, tc.tool, args)

			httpCmp := extractComparable(t, httpEnv)
			mcpCmp := extractComparable(t, mcpEnv)
			requireParity(t, "http", "mcp", httpCmp, mcpCmp)

			if !httpCmp.hasError {
				t.Fatalf("%s http surface returned a success answer, want unsupported_capability", tc.name)
			}
			if got, want := httpCmp.errorCode, query.ErrorCodeUnsupportedCapability; got != want {
				t.Fatalf("%s error code = %q, want %q", tc.name, got, want)
			}
			if got, want := httpCmp.errorCapability, tc.capability; got != want {
				t.Fatalf("%s error capability = %q, want %q", tc.name, got, want)
			}
			// Neither surface may leak a confident truth level on the refusal path.
			if httpCmp.truthLevel != "" || mcpCmp.truthLevel != "" {
				t.Fatalf("%s unsupported answer leaked truth level: http=%q mcp=%q", tc.name, httpCmp.truthLevel, mcpCmp.truthLevel)
			}
			if !strings.Contains(summary, string(query.ErrorCodeUnsupportedCapability)) {
				t.Fatalf("%s summary = %q, want it to surface %q", tc.name, summary, query.ErrorCodeUnsupportedCapability)
			}
		})
	}
}
