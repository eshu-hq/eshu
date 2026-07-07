// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestOpenAPIIncludesWorkItemEvidenceRoute(t *testing.T) {
	t.Parallel()

	spec := OpenAPISpec()
	for _, want := range []string{
		`"/api/v0/work-items/evidence"`,
		`"operationId": "listWorkItemEvidence"`,
		`"work_item_key"`,
		`"external_url"`,
		`"url_fingerprint"`,
		`"missing_evidence"`,
		`"metadata_type"`,
		`"warning_reason"`,
		`"provider_id_fingerprint"`,
		`"metadata_warning"`,
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("OpenAPISpec() missing %q", want)
		}
	}
}
