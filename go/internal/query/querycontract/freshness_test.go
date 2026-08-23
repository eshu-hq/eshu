// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

func TestFreshnessCauseNextCheckCoversEveryEnumValue(t *testing.T) {
	for cause := range freshnessCauses {
		check, ok := FreshnessCauseNextCheck(cause)
		if !ok {
			t.Fatalf("cause %q has no next-check mapping", cause)
		}
		if check.Tool == "" && check.Route == "" {
			t.Fatalf("cause %q next-check has neither tool nor route", cause)
		}
		if check.Reason == "" {
			t.Fatalf("cause %q next-check has no reason", cause)
		}
	}
}
