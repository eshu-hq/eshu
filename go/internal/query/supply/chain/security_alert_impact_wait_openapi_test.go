// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/openapi"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// TestSecurityAlertReconciliationImpactWaitOutcomeOpenAPIEnum keeps the
// published row contract in lockstep with the reducer's closed persisted
// give-up set (#8956).
func TestSecurityAlertReconciliationImpactWaitOutcomeOpenAPIEnum(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(openapi.Spec()), &spec); err != nil {
		t.Fatalf("decode OpenAPI spec: %v", err)
	}
	row := lookupMap(t, spec, "paths", "/api/v0/supply-chain/security-alerts/reconciliations", "get", "responses", "200",
		"content", "application/json", "schema", "properties", "reconciliations", "items", "properties")
	got := enumStrings(t, lookupMap(t, row, "impact_wait_outcome"))
	slices.Sort(got)
	want := securityalert.PersistedImpactWaitOutcomes()
	if !slices.Equal(got, want) {
		t.Fatalf("impact_wait_outcome enum = %v, want %v", got, want)
	}
}
