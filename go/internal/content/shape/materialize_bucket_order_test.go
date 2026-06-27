// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import "testing"

// TestContentEntityBucketsHelmTemplateValuesAppendedAtEnd guards the
// frozen-table invariant documented in AGENTS.md: a newly added entity bucket
// MUST be appended to the end of contentEntityBuckets, never inserted mid-table.
// Inserting before the SQL/data/impl_blocks/pagerduty/gitlab buckets would shift
// the persisted entity row order for every later label. The Helm template-value
// buckets are the most recent addition, so they must be the trailing entries,
// with the GitLab buckets immediately before them.
func TestContentEntityBucketsHelmTemplateValuesAppendedAtEnd(t *testing.T) {
	t.Parallel()

	n := len(contentEntityBuckets)
	if n < 4 {
		t.Fatalf("contentEntityBuckets too short: %d", n)
	}

	helmTemplateValues := map[string]string{
		"helm_value_definitions":     "HelmValueDefinition",
		"helm_template_value_usages": "HelmTemplateValueUsage",
	}

	// The two trailing entries must be the Helm template-value buckets (order
	// between the two is fixed: definitions before usages).
	last := contentEntityBuckets[n-1]
	penultimate := contentEntityBuckets[n-2]
	if penultimate.bucket != "helm_value_definitions" || penultimate.label != "HelmValueDefinition" {
		t.Fatalf("penultimate bucket = %q/%q, want helm_value_definitions/HelmValueDefinition (append at end)", penultimate.bucket, penultimate.label)
	}
	if last.bucket != "helm_template_value_usages" || last.label != "HelmTemplateValueUsage" {
		t.Fatalf("last bucket = %q/%q, want helm_template_value_usages/HelmTemplateValueUsage (append at end)", last.bucket, last.label)
	}

	// The GitLab buckets (previous most-recent addition) must remain immediately
	// before the Helm template-value buckets, never shifted.
	if got := contentEntityBuckets[n-3]; got.bucket != "gitlab_jobs" || got.label != "GitlabJob" {
		t.Fatalf("bucket at n-3 = %q/%q, want gitlab_jobs/GitlabJob (gitlab stays before helm template values)", got.bucket, got.label)
	}
	if got := contentEntityBuckets[n-4]; got.bucket != "gitlab_pipelines" || got.label != "GitlabPipeline" {
		t.Fatalf("bucket at n-4 = %q/%q, want gitlab_pipelines/GitlabPipeline", got.bucket, got.label)
	}

	// No Helm template-value bucket may appear anywhere else in the table.
	for i := 0; i < n-2; i++ {
		if _, isHelm := helmTemplateValues[contentEntityBuckets[i].bucket]; isHelm {
			t.Fatalf("Helm template-value bucket %q found mid-table at index %d; it must be appended at the end", contentEntityBuckets[i].bucket, i)
		}
	}
}
