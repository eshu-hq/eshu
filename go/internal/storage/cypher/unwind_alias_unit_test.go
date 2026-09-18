// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "testing"

// TestHasUnwindVariableReusedAsReturnAlias is the seeded RED/GREEN proof for
// the X9 guard.
func TestHasUnwindVariableReusedAsReturnAlias(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "retraction lookup shape",
			value: "UNWIND $repo_ids AS repo_id\nMATCH (i:WorkloadInstance {repo_id: repo_id})\nRETURN DISTINCT i.repo_id AS repo_id, i.id AS instance_id",
			want:  true,
		},
		{
			name:  "hydration shape with OPTIONAL MATCH",
			value: "UNWIND $entity_ids AS entity_id MATCH (e) WHERE e.id = entity_id OPTIONAL MATCH (r)-[:DEFINES]->(e) RETURN e.id AS entity_id, r.id AS repo_id",
			want:  true,
		},
		{
			name:  "bare UNWIND variable returned after MATCH",
			value: "UNWIND $entity_ids AS entity_id\nMATCH (e) WHERE e.id = entity_id\nRETURN entity_id,\n       e.name AS name",
			want:  true,
		},
		{
			name:  "DISTINCT bare UNWIND variable",
			value: "UNWIND $ids AS repo_id MATCH (r {id: repo_id}) RETURN DISTINCT repo_id ORDER BY repo_id",
			want:  true,
		},
		{
			name:  "UNWIND variable only in ORDER BY is not a column",
			value: "UNWIND $ids AS repo_id MATCH (r {id: repo_id}) RETURN r.id AS id ORDER BY repo_id",
			want:  false,
		},
		{
			name:  "renamed UNWIND variable",
			value: "UNWIND $repo_ids AS requested_repo_id MATCH (i:WorkloadInstance {repo_id: requested_repo_id}) RETURN i.repo_id AS repo_id",
			want:  false,
		},
		{
			name:  "UNWIND variable returned under a different alias",
			value: "UNWIND $digests AS candidate_digest MATCH (img {digest: candidate_digest}) RETURN candidate_digest AS matched_digest",
			want:  false,
		},
		{
			name:  "no MATCH between UNWIND and RETURN",
			value: "UNWIND ['a'] AS x RETURN x AS x",
			want:  false,
		},
		{
			name:  "UNION branches with independent aliases",
			value: "CALL { UNWIND $ids AS repo_id MATCH (s {id: repo_id}) RETURN s.id AS source_repo_id UNION UNWIND $ids AS repo_id MATCH (t {id: repo_id}) RETURN t.id AS source_repo_id } RETURN source_repo_id",
			want:  false,
		},
		{
			name:  "alias match is a whole word only",
			value: "UNWIND $ids AS repo MATCH (r {id: repo}) RETURN r.id AS repo_id",
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasUnwindVariableReusedAsReturnAlias(tc.value); got != tc.want {
				t.Fatalf("hasUnwindVariableReusedAsReturnAlias(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
