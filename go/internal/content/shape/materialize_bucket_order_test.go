// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import "testing"

// TestContentEntityBucketsGitlabAppendedAtEnd guards the frozen-table invariant
// documented in AGENTS.md: a newly added entity bucket MUST be appended to the
// end of contentEntityBuckets, never inserted mid-table. Inserting before the
// SQL/data/impl_blocks/pagerduty buckets would shift the persisted entity row
// order for every later label. The GitLab buckets are the most recent addition,
// so they must be the trailing entries.
func TestContentEntityBucketsGitlabAppendedAtEnd(t *testing.T) {
	t.Parallel()

	n := len(contentEntityBuckets)
	if n < 2 {
		t.Fatalf("contentEntityBuckets too short: %d", n)
	}

	gitlab := map[string]string{
		"gitlab_pipelines": "GitlabPipeline",
		"gitlab_jobs":      "GitlabJob",
	}

	// The two trailing entries must be the GitLab buckets (order between the two
	// is fixed: pipelines before jobs).
	last := contentEntityBuckets[n-1]
	penultimate := contentEntityBuckets[n-2]
	if penultimate.bucket != "gitlab_pipelines" || penultimate.label != "GitlabPipeline" {
		t.Fatalf("penultimate bucket = %q/%q, want gitlab_pipelines/GitlabPipeline (append at end)", penultimate.bucket, penultimate.label)
	}
	if last.bucket != "gitlab_jobs" || last.label != "GitlabJob" {
		t.Fatalf("last bucket = %q/%q, want gitlab_jobs/GitlabJob (append at end)", last.bucket, last.label)
	}

	// No GitLab bucket may appear anywhere else in the table.
	for i := 0; i < n-2; i++ {
		if _, isGitlab := gitlab[contentEntityBuckets[i].bucket]; isGitlab {
			t.Fatalf("GitLab bucket %q found mid-table at index %d; it must be appended at the end", contentEntityBuckets[i].bucket, i)
		}
	}
}
