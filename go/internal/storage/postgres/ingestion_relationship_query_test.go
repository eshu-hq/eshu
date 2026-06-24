// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestListLatestRelationshipFactRecordsQueryIncludesGCPRelationshipFacts(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listLatestRelationshipFactRecordsQuery, "'gcp_cloud_relationship'") {
		t.Fatalf(
			"listLatestRelationshipFactRecordsQuery must backfill GCP relationship facts:\n%s",
			listLatestRelationshipFactRecordsQuery,
		)
	}
}
