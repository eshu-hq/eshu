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
		// The bound is PER DIGEST now: a LATERAL driven by the distinct digest
		// set, so each digest gets its own bounded ordered index scan (#5789).
		"WITH wanted AS (",
		"SELECT DISTINCT unnest($1::text[]) AS digest",
		"CROSS JOIN LATERAL (",
		"owner.winning_row->>'running_image_digest' = wanted.digest",
		", candidates AS MATERIALIZED (",
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
		t.Fatalf("args = %#v, want digest, repository grants, scope grants, and per-digest limit", args)
	}
	if got, want := args[3], supplyChainCloudRuntimeProbePerDigestMaxResults; got != want {
		t.Fatalf("bound arg = %v, want the PER-DIGEST limit %v, not the total-row cap", got, want)
	}

	// A global cap over the whole ordered set does not share: on a skewed
	// corpus it spends the entire budget on one hot digest and returns nothing
	// for the rest. This is the assertion that fails if the LATERAL is ever
	// flattened back into one scan with a single trailing LIMIT.
	if strings.Contains(query, "= ANY($1::text[])") {
		t.Fatalf("digest match must be per-digest (= wanted.digest), not a shared ANY() scan:\n%s", query)
	}
	lateral := strings.Index(query, "CROSS JOIN LATERAL (")
	perDigestLimit := strings.Index(query, "LIMIT $4")
	authorization := strings.Index(query, "fact.fact_id = candidate.source_fact_id")
	if lateral == -1 || perDigestLimit == -1 || authorization == -1 {
		t.Fatalf("query is missing the LATERAL, its bound, or the authorization boundary:\n%s", query)
	}
	if lateral >= perDigestLimit || perDigestLimit >= authorization {
		t.Fatalf("the per-digest LIMIT must sit inside the LATERAL and run before authorization:\n%s", query)
	}
	if count := strings.Count(query, "LIMIT $4"); count != 1 {
		t.Fatalf("runtime-digest query bound count = %d, want exactly one per-digest bound:\n%s", count, query)
	}
}
