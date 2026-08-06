// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestBuildServiceResultLimitsNamesTheEvidenceFileReadBound is a PR #5933
// review fix (Codex, service_story_dossier.go:308).
// consumer_repositories_truncated can fire purely because the service
// repository's own indexed-file list hit serviceEvidenceFileLimit
// (service_evidence_types.go) -- a bound entirely separate from the 25-row
// downstream_read_limit fan-out bound in the same block -- so a service with
// fewer than 25 dependents/consumers can still report truncated: true for
// that reason alone. Before this fix, result_limits exposed only
// downstream_read_limit, so a caller who saw truncated: true next to
// graph_dependent_count/content_consumer_count well under
// downstream_read_limit had no way to learn that a different, much larger
// bound (the file list) was the one that actually fired. Split into its own
// file (rather than deployment_trace_truncation_disclosure_test.go, its
// nearest sibling) to keep that file under the repository's 500-line cap.
func TestBuildServiceResultLimitsNamesTheEvidenceFileReadBound(t *testing.T) {
	t.Parallel()

	limits := buildServiceResultLimitsWithContext(newServiceStoryBuildContext(map[string]any{"name": "orders-api"}))
	if got, want := IntVal(limits, "evidence_file_read_limit"), serviceEvidenceFileLimit; got != want {
		t.Fatalf("result_limits[evidence_file_read_limit] = %d, want %d", got, want)
	}
}

// TestServiceInvestigationCoverageNamesTheEvidenceFileReadBound is the
// investigate_service sibling of the fix above: coverage_summary carries the
// same downstream_read_limit field (service_investigation.go, line ~461) and
// needs the same second bound named for the same reason.
func TestServiceInvestigationCoverageNamesTheEvidenceFileReadBound(t *testing.T) {
	t.Parallel()

	packet := buildServiceInvestigationPacket("orders-api", map[string]any{
		"name":      "orders-api",
		"repo_id":   "repository:orders",
		"repo_name": "orders-api",
	}, serviceInvestigationOptions{})
	coverage := mapValue(packet, "coverage_summary")
	if got, want := IntVal(coverage, "evidence_file_read_limit"), serviceEvidenceFileLimit; got != want {
		t.Fatalf("coverage_summary[evidence_file_read_limit] = %d, want %d", got, want)
	}
}
