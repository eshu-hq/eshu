// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import "testing"

// TestContentEntityBucketsHelmTemplateValuesAppendedAtEnd guards the
// frozen-table invariant documented in AGENTS.md: a newly added entity bucket
// MUST be appended to the end of contentEntityBuckets, never inserted mid-table.
// Inserting before the SQL/data/impl_blocks/pagerduty/gitlab buckets would shift
// the persisted entity row order for every later label. sql_migrations
// (SqlMigration, #5346) is now the most recent addition, so it must be the
// trailing entry, with the Helm template-value buckets immediately before it.
func TestContentEntityBucketsHelmTemplateValuesAppendedAtEnd(t *testing.T) {
	t.Parallel()

	n := len(contentEntityBuckets)
	if n < 5 {
		t.Fatalf("contentEntityBuckets too short: %d", n)
	}

	helmTemplateValues := map[string]string{
		"helm_value_definitions":     "HelmValueDefinition",
		"helm_template_value_usages": "HelmTemplateValueUsage",
	}

	// The trailing entry must be sql_migrations (#5346).
	last := contentEntityBuckets[n-1]
	if last.bucket != "sql_migrations" || last.label != "SqlMigration" {
		t.Fatalf("last bucket = %q/%q, want sql_migrations/SqlMigration (append at end)", last.bucket, last.label)
	}

	// The Helm template-value buckets (previous most-recent addition) must
	// remain immediately before sql_migrations, never shifted.
	penultimate := contentEntityBuckets[n-2]
	if penultimate.bucket != "helm_template_value_usages" || penultimate.label != "HelmTemplateValueUsage" {
		t.Fatalf("penultimate bucket = %q/%q, want helm_template_value_usages/HelmTemplateValueUsage (append at end)", penultimate.bucket, penultimate.label)
	}
	if got := contentEntityBuckets[n-3]; got.bucket != "helm_value_definitions" || got.label != "HelmValueDefinition" {
		t.Fatalf("bucket at n-3 = %q/%q, want helm_value_definitions/HelmValueDefinition", got.bucket, got.label)
	}

	// The GitLab buckets must remain immediately before the Helm template-value
	// buckets, never shifted.
	if got := contentEntityBuckets[n-4]; got.bucket != "gitlab_jobs" || got.label != "GitlabJob" {
		t.Fatalf("bucket at n-4 = %q/%q, want gitlab_jobs/GitlabJob (gitlab stays before helm template values)", got.bucket, got.label)
	}
	if got := contentEntityBuckets[n-5]; got.bucket != "gitlab_pipelines" || got.label != "GitlabPipeline" {
		t.Fatalf("bucket at n-5 = %q/%q, want gitlab_pipelines/GitlabPipeline", got.bucket, got.label)
	}

	// No Helm template-value bucket, and no sql_migrations bucket, may appear
	// anywhere else in the table.
	for i := 0; i < n-3; i++ {
		if _, isHelm := helmTemplateValues[contentEntityBuckets[i].bucket]; isHelm {
			t.Fatalf("Helm template-value bucket %q found mid-table at index %d; it must be appended at the end", contentEntityBuckets[i].bucket, i)
		}
	}
	for i := 0; i < n-1; i++ {
		if contentEntityBuckets[i].bucket == "sql_migrations" {
			t.Fatalf("sql_migrations found mid-table at index %d; it must be the last entry", i)
		}
	}
}
