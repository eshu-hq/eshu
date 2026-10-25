// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestScopeGenerationPayloadRecordsSecurityAlertTargetSet proves a
// security-alert generation's payload carries the repository set it collected
// and that set's hash (#8838), so the query path can report a repository added
// to the target after this generation as not yet collected.
func TestScopeGenerationPayloadRecordsSecurityAlertTargetSet(t *testing.T) {
	t.Parallel()

	set := scope.NewSecurityAlertTargetSet([]string{"acme/b", "acme/a"})
	raw := scopeGenerationPayload(scope.ScopeGeneration{SecurityAlertTargetSet: set})

	var payload struct {
		TargetSet []string `json:"security_alert_target_set"`
		Hash      string   `json:"security_alert_target_set_hash"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("payload %q is not JSON: %v", raw, err)
	}
	if want := []string{"acme/a", "acme/b"}; !slices.Equal(payload.TargetSet, want) {
		t.Fatalf("security_alert_target_set = %v, want %v (payload %s)", payload.TargetSet, want, raw)
	}
	if payload.Hash != set.Hash() || payload.Hash == "" {
		t.Fatalf("security_alert_target_set_hash = %q, want %q", payload.Hash, set.Hash())
	}
}

// TestScopeGenerationPayloadRecordsAnEmptySecurityAlertTargetSet keeps a
// recorded empty set distinct from no set: an empty array covers nothing.
func TestScopeGenerationPayloadRecordsAnEmptySecurityAlertTargetSet(t *testing.T) {
	t.Parallel()

	raw := scopeGenerationPayload(scope.ScopeGeneration{SecurityAlertTargetSet: scope.NewSecurityAlertTargetSet(nil)})
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("payload %q is not JSON: %v", raw, err)
	}
	if got := string(payload["security_alert_target_set"]); got != "[]" {
		t.Fatalf("security_alert_target_set = %q, want [] (payload %s)", got, raw)
	}
}

// TestScopeGenerationPayloadOmitsSecurityAlertTargetSetForOtherCollectors keeps
// every other generation's payload unchanged.
func TestScopeGenerationPayloadOmitsSecurityAlertTargetSetForOtherCollectors(t *testing.T) {
	t.Parallel()

	if got := scopeGenerationPayload(scope.ScopeGeneration{}); got != "{}" {
		t.Fatalf("payload = %q, want {}", got)
	}
}
