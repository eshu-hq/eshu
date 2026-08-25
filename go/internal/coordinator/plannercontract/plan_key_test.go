// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package plannercontract

import "testing"

func TestValidateSafePlanKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		owner   string
		planKey string
		wantErr string
	}{
		{name: "blank", owner: "test planner", wantErr: "test planner plan_key must not be blank"},
		{name: "whitespace only", owner: "test planner", planKey: " \t\n", wantErr: "test planner plan_key must not be blank"},
		{name: "letters digits and separators", owner: "test planner", planKey: "Bucket-1.alpha_beta"},
		{name: "surrounding whitespace", owner: "test planner", planKey: "  Bucket-1.alpha_beta \n"},
		{name: "slash", owner: "test planner", planKey: "bucket/source", wantErr: "test planner plan_key must not include raw source locator material"},
		{name: "backslash", owner: "test planner", planKey: `bucket\source`, wantErr: "test planner plan_key must not include raw source locator material"},
		{name: "embedded space", owner: "test planner", planKey: "bucket source", wantErr: "test planner plan_key contains unsupported character ' '"},
		{name: "colon", owner: "test planner", planKey: "bucket:source", wantErr: "test planner plan_key contains unsupported character ':'"},
		{name: "unicode", owner: "test planner", planKey: "bucket-é", wantErr: "test planner plan_key contains unsupported character 'é'"},
		{name: "owner appears in error", owner: "scanner-worker planner", planKey: "bad:key", wantErr: "scanner-worker planner plan_key contains unsupported character ':'"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateSafePlanKey(test.owner, test.planKey)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSafePlanKey(%q, %q) error = %v, want nil", test.owner, test.planKey, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateSafePlanKey(%q, %q) error = nil, want %q", test.owner, test.planKey, test.wantErr)
			}
			if err.Error() != test.wantErr {
				t.Fatalf("ValidateSafePlanKey(%q, %q) error = %q, want %q", test.owner, test.planKey, err, test.wantErr)
			}
		})
	}
}
