// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import "testing"

// Report.Findings documents one finding per statement, variable, and kind, the
// same key IDWrites counts by. A statement that writes the id of one node
// variable twice in the same kind therefore reports once, while a second
// variable or a second kind on the same variable reports separately.
func TestFindingsAreOnePerStatementVariableAndKind(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantWrites int
		wantKeys   []string
	}{
		{
			name:       "two SET id occurrences on one variable report once",
			text:       "MERGE (n:Unconstrained {uid: $uid}) ON CREATE SET n.id = $uid ON MATCH SET n.id = $uid",
			wantWrites: 1,
			wantKeys:   []string{"n/set_property"},
		},
		{
			name:       "a map key and a SET on one variable report once each",
			text:       "MERGE (n:Unconstrained {id: $id}) ON CREATE SET n.id = $id",
			wantWrites: 2,
			wantKeys:   []string{"n/map_key", "n/set_property"},
		},
		{
			name:       "two variables report separately",
			text:       "MERGE (a:Unconstrained {id: $a}) MERGE (b:Unconstrained {id: $b})",
			wantWrites: 2,
			wantKeys:   []string{"a/map_key", "b/map_key"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := check(t, tc.text, "")
			if report.IDWrites != tc.wantWrites {
				t.Fatalf("IDWrites = %d, want %d: %+v", report.IDWrites, tc.wantWrites, report.Findings)
			}
			var got []string
			seen := map[string]int{}
			for _, f := range report.Findings {
				key := f.Variable + "/" + f.Kind
				seen[key]++
				got = append(got, key)
			}
			for key, n := range seen {
				if n != 1 {
					t.Errorf("finding %s reported %d times, want once: %v", key, n, got)
				}
			}
			if len(got) != len(tc.wantKeys) {
				t.Fatalf("findings = %v, want %v", got, tc.wantKeys)
			}
			for _, want := range tc.wantKeys {
				if seen[want] != 1 {
					t.Errorf("findings = %v lack %s", got, want)
				}
			}
		})
	}
}
