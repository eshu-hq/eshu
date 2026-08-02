// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestContainerImageIdentityListQueryFoldsAuthorizedCurrentSupports(t *testing.T) {
	t.Parallel()

	query := listContainerImageIdentitiesQuery
	for _, want := range []string{
		"FROM container_image_identity_current_supports",
		"source_repository_ids && $8::text[]",
		"PARTITION BY digest",
		"identity_id > $6",
		"ORDER BY winner.identity_id ASC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("list query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "FROM fact_records") {
		t.Fatalf("v3 list query must not read legacy fact_records:\n%s", query)
	}
	authPos := strings.Index(query, "source_repository_ids && $8::text[]")
	foldPos := strings.Index(query, "PARTITION BY digest")
	if authPos < 0 || foldPos < 0 || authPos > foldPos {
		t.Fatalf("authorization must filter supports before canonical folding:\n%s", query)
	}
}

func TestContainerImageIdentityAggregateQueriesCountCanonicalDigests(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"total":     containerImageIdentityAggregateTotalQuery,
		"group":     containerImageIdentityAggregateGroupQueryTemplate,
		"inventory": containerImageIdentityInventoryQueryTemplate,
	} {
		if !strings.Contains(query, "container_image_identity_current_supports") {
			t.Fatalf("%s query does not read current v3 supports:\n%s", name, query)
		}
		if strings.Contains(query, "FROM fact_records") {
			t.Fatalf("%s query still reads legacy fact_records:\n%s", name, query)
		}
		if !strings.Contains(query, "source_repository_ids && $6::text[]") {
			t.Fatalf("%s query does not authorize supports before folding:\n%s", name, query)
		}
	}
	if !strings.Contains(containerImageIdentityAggregateTotalQuery, "COUNT(DISTINCT digest)") {
		t.Fatalf("total query must count one logical identity per digest:\n%s", containerImageIdentityAggregateTotalQuery)
	}
	if !strings.Contains(containerImageIdentityInventoryQueryTemplate, "COUNT(DISTINCT digest)") {
		t.Fatalf("repository inventory must count each digest once per visible association:\n%s", containerImageIdentityInventoryQueryTemplate)
	}
}

func TestContainerImageIdentityCanonicalWinnerBreaksCrossScopeTies(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"list":      listContainerImageIdentitiesQuery,
		"aggregate": containerImageIdentityAggregateGroupQueryTemplate,
	} {
		if !strings.Contains(query, "repository_id,\n                image_ref,\n                scope_id,\n                support_id") {
			t.Fatalf("%s canonical ranking lacks a deterministic cross-scope tie-break:\n%s", name, query)
		}
	}
}
