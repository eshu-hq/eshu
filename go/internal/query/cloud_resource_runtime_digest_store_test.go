// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestBuildCloudResourceRuntimeDigestQueryUsesIndexedDigestAndCurrentAuthorization(t *testing.T) {
	t.Parallel()

	query, args := buildCloudResourceRuntimeDigestQuery(
		[]string{"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		false,
		[]string{"repository:r_allowed"},
		[]string{"scope:allowed"},
	)
	for _, want := range []string{
		"WITH candidates AS MATERIALIZED",
		"owner.winning_row->>'running_image_digest' = ANY($1::text[])",
		"NULLIF(BTRIM(owner.winning_row->>'running_image_digest'), '') IS NOT NULL",
		"NULLIF(BTRIM(owner.winning_row->>'arn'), '') IS NOT NULL",
		"fact.fact_id = candidate.source_fact_id",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"scope.source_key = ANY($2::text[])",
		"fact.scope_id = ANY($3::text[])",
		"ORDER BY owner.winning_row->>'running_image_digest', owner.winning_row->>'arn', owner.uid",
		"LIMIT $4",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q:\n%s", want, query)
		}
	}
	if len(args) != 4 {
		t.Fatalf("args = %#v, want digest, repository grants, scope grants, and limit", args)
	}

	candidateLimit := strings.Index(query, "LIMIT $4")
	authorization := strings.Index(query, "fact.fact_id = candidate.source_fact_id")
	if candidateLimit == -1 || authorization == -1 {
		t.Fatalf("query is missing candidate limit or authorization boundary:\n%s", query)
	}
	if candidateLimit > authorization {
		t.Fatalf("candidate LIMIT must run before authorization to bound owner-ledger work:\n%s", query)
	}
	if count := strings.Count(query, "LIMIT $4"); count != 1 {
		t.Fatalf("runtime-digest query LIMIT count = %d, want exactly one candidate bound:\n%s", count, query)
	}
}
