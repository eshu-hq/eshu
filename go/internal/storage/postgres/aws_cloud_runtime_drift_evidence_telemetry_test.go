// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// summaryRecordingQueryer records the db.query.summary label each query
// carried on its context, then delegates to the fake.
type summaryRecordingQueryer struct {
	*fakeExecQueryer
	summaries []string
}

// QueryContext records the read label and delegates.
func (q *summaryRecordingQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	q.summaries = append(q.summaries, db.QuerySummaryFromContext(ctx))
	return q.fakeExecQueryer.QueryContext(ctx, query, args...)
}

// TestPostgresAWSCloudRuntimeDriftEvidenceLoaderLabelsDriftReads proves each
// drift evidence statement names itself on the postgres.query span
// (db.query.summary), so an operator looking at a slow
// reducer.aws_runtime_drift_evidence_load span can tell the AWS resource read
// from the ARN-keyed state join (#7666). Unlabeled, the two reads were
// indistinguishable children of the load span.
func TestPostgresAWSCloudRuntimeDriftEvidenceLoaderLabelsDriftReads(t *testing.T) {
	t.Parallel()

	const arn = "arn:aws:s3:::labelled"
	recorder := &summaryRecordingQueryer{fakeExecQueryer: &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{arn, []byte(`{"arn":"` + arn + `","resource_id":"labelled","resource_type":"aws_s3_bucket"}`)}}},
			{rows: [][]any{}},
		},
	}}
	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{
		DB:             recorder,
		ConfigResolver: &stubAWSRuntimeDriftConfigResolver{},
	}

	if _, err := loader.LoadAWSCloudRuntimeDriftEvidence(context.Background(), "aws:scope", "aws-gen"); err != nil {
		t.Fatalf("LoadAWSCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	want := []string{"aws_runtime_drift_resources", "aws_runtime_drift_active_state_by_arn"}
	if len(recorder.summaries) != len(want) {
		t.Fatalf("query summaries = %q, want %q", recorder.summaries, want)
	}
	for i := range want {
		if recorder.summaries[i] != want[i] {
			t.Fatalf("query %d summary = %q, want %q", i, recorder.summaries[i], want[i])
		}
	}
}
