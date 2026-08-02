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
		"$3::text[]",
		"$4::text[]",
		"$5::text[]",
		"$6::text",
		"$7::integer",
	} {
		if !strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, want) {
			t.Fatalf("listCurrentContainerImageIdentitySupportFactsQuery missing %q", want)
		}
	}
	for _, forbidden := range []string{"FROM fact_records", "WHERE", "ORDER BY", "LIMIT"} {
		if strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, forbidden) {
			t.Fatalf("direct identity loader contains outer %q operation:\n%s", forbidden, listCurrentContainerImageIdentitySupportFactsQuery)
		}
	}
	if strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, "container_image_identity_current_facts AS") {
		t.Fatalf("CI/CD identity loader must use the bounded current-facts function:\n%s", listCurrentContainerImageIdentitySupportFactsQuery)
	}
	if strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, "container_image_identity_current_facts_for(") {
		t.Fatalf("CI/CD reducer loader must preserve support-grain correlation:\n%s", listCurrentContainerImageIdentitySupportFactsQuery)
	}
}
