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
		"owner.winning_row->>'running_image_digest' = ANY($1::text[])",
		"fact.fact_id = owner.winning_row->>'source_fact_id'",
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
}
