// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestListActiveCICDRunCorrelationFactsQueryIsArtifactBoundedAndPaged(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM container_image_identity_current_support_facts_for(",
		"$1::text[]",
		"$2::text[]",
		"'{}'::text[]",
		"$3::text",
		"$4::integer",
		"($3 = '' OR fact.fact_id > $3)",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $4",
	} {
		if !strings.Contains(listActiveCICDRunCorrelationFactsQuery, want) {
			t.Fatalf("listActiveCICDRunCorrelationFactsQuery missing %q", want)
		}
	}
	if strings.Contains(listActiveCICDRunCorrelationFactsQuery, "FROM fact_records") {
		t.Fatalf("CI/CD identity loader must not read legacy fact_records:\n%s", listActiveCICDRunCorrelationFactsQuery)
	}
	if strings.Contains(listActiveCICDRunCorrelationFactsQuery, "container_image_identity_current_facts AS") {
		t.Fatalf("CI/CD identity loader must use the bounded current-facts function:\n%s", listActiveCICDRunCorrelationFactsQuery)
	}
	if strings.Contains(listActiveCICDRunCorrelationFactsQuery, "container_image_identity_current_facts_for(") {
		t.Fatalf("CI/CD reducer loader must preserve support-grain correlation:\n%s", listActiveCICDRunCorrelationFactsQuery)
	}
}
