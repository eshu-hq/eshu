// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import "testing"

func TestDotNetGlobalEvidenceShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		state            string
		rejected         int
	}{
		{"exact", `{"sdk":{"version":"8.0.300"}}`, "8.0.300", "parsed_values", 0},
		{"range", `{"sdk":{"version":">=8.0 <9.0"}}`, ">=8.0 <9.0", "parsed_values", 0},
		{"alias", `{"sdk":{"version":"preview"}}`, "preview", "parsed_values", 0},
		{"placeholder", `{"sdk":{"version":"${SDK}"}}`, "${SDK}", "parsed_values", 0},
		{"credential", `{"sdk":{"version":"https://user:password@example.invalid"}}`, "", "parsed_empty", 1},
		{"malformed", `{"sdk":`, "", "unavailable", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := ExtractDotNetGlobalEvidence([]byte(tc.body))
			if e.State != tc.state || e.Rejected != tc.rejected || len(e.Values) != btoi(tc.want != "") {
				t.Fatalf("evidence = %+v", e)
			}
			if tc.want != "" && (e.Values[0].Value != tc.want || e.Values[0].Line != 1) {
				t.Fatalf("value = %+v", e.Values)
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}
