// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/openapi"
)

// TestSecurityAlertCoverageOpenAPIEnum keeps the published coverage contract in
// lockstep with the handler (#8803): every security-alert read route documents
// coverage.state with not_configured and coverage.reasons with exactly the
// closed reason set the handler can emit.
func TestSecurityAlertCoverageOpenAPIEnum(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(openapi.Spec()), &spec); err != nil {
		t.Fatalf("decode OpenAPI spec: %v", err)
	}
	wantStates := []string{securityAlertCoverageComplete, securityAlertCoverageNotConfigured, securityAlertCoverageTargetIncomplete}
	wantReasons := securityAlertCoverageReasons()
	for _, path := range []string{
		"/api/v0/supply-chain/security-alerts/reconciliations",
		"/api/v0/supply-chain/security-alerts/reconciliations/count",
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory",
	} {
		coverage := lookupMap(t, spec, "paths", path, "get", "responses", "200", "content", "application/json", "schema", "properties", "coverage", "properties")
		gotStates := enumStrings(t, lookupMap(t, coverage, "state"))
		slices.Sort(gotStates)
		if !slices.Equal(gotStates, wantStates) {
			t.Fatalf("%s coverage.state enum = %v, want %v", path, gotStates, wantStates)
		}
		gotReasons := enumStrings(t, lookupMap(t, coverage, "reasons", "items"))
		slices.Sort(gotReasons)
		if !slices.Equal(gotReasons, wantReasons) {
			t.Fatalf("%s coverage.reasons enum = %v, want %v", path, gotReasons, wantReasons)
		}
		properties := lookupMap(t, spec, "paths", path, "get", "responses", "200", "content", "application/json", "schema", "properties")
		if _, ok := properties["collector_readiness"]; !ok {
			t.Fatalf("%s does not document collector_readiness", path)
		}
	}
}

func lookupMap(t *testing.T, root map[string]any, keys ...string) map[string]any {
	t.Helper()
	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI key %q missing under %v", key, keys)
		}
		current = next
	}
	return current
}

func enumStrings(t *testing.T, schema map[string]any) []string {
	t.Helper()
	raw, ok := schema["enum"].([]any)
	if !ok {
		t.Fatalf("schema has no enum: %v", schema)
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		out = append(out, value.(string))
	}
	return out
}
