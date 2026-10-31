// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
)

// TestSeverityBucketSQLMatchesGoBucketOnRealPostgres executes cvss.BucketSQL,
// the expression both the winners recompute and the read-time findings queries
// embed, on a real Postgres and compares every row to the Go derivation
// (#9693). It is the execution half of the bucket parity; the text half lives
// beside each SQL site. It needs only a reachable Postgres, no schema.
func TestSeverityBucketSQLMatchesGoBucketOnRealPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live severity bucket parity proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	cases := []struct {
		name    string
		payload string
		score   float64
		label   string
	}{
		{"critical score", `{"cvss_score": 9.8}`, 9.8, ""},
		{"score 9.0 boundary", `{"cvss_score": 9.0}`, 9.0, ""},
		{"score 8.9", `{"cvss_score": 8.9}`, 8.9, ""},
		{"score 7.0 boundary", `{"cvss_score": 7.0}`, 7.0, ""},
		{"score 6.9", `{"cvss_score": 6.9}`, 6.9, ""},
		{"score 4.0 boundary", `{"cvss_score": 4.0}`, 4.0, ""},
		{"score 3.9", `{"cvss_score": 3.9}`, 3.9, ""},
		{"score 0.1", `{"cvss_score": 0.1}`, 0.1, ""},
		{"score zero no label", `{"cvss_score": 0}`, 0, ""},
		{"score as string", `{"cvss_score": "7.5"}`, 7.5, ""},
		{"empty score string", `{"cvss_score": ""}`, 0, ""},
		{"no payload fields", `{}`, 0, ""},
		{"score beats a conflicting label", `{"cvss_score": 5.0, "provenance": {"selected_severity_label": "CRITICAL"}}`, 5.0, "CRITICAL"},
		{"label critical", `{"provenance": {"selected_severity_label": "CRITICAL"}}`, 0, "CRITICAL"},
		{"label high", `{"provenance": {"selected_severity_label": "high"}}`, 0, "high"},
		{"label important", `{"provenance": {"selected_severity_label": "Important"}}`, 0, "Important"},
		{"label moderate", `{"provenance": {"selected_severity_label": "MODERATE"}}`, 0, "MODERATE"},
		{"label medium", `{"provenance": {"selected_severity_label": "medium"}}`, 0, "medium"},
		{"label low padded", `{"provenance": {"selected_severity_label": "  LOW "}}`, 0, "  LOW "},
		{"label with zero score", `{"cvss_score": 0, "provenance": {"selected_severity_label": "high"}}`, 0, "high"},
		{"unrecognized label", `{"provenance": {"selected_severity_label": "severe"}}`, 0, "severe"},
		{"none label", `{"provenance": {"selected_severity_label": "none"}}`, 0, "none"},
		{"empty label", `{"provenance": {"selected_severity_label": ""}}`, 0, ""},
		{"provenance without a label", `{"provenance": {"selected_severity_vector": "x"}}`, 0, ""},
	}
	for _, alias := range []string{"fact.payload", "source.payload"} {
		table := alias[:len(alias)-len(".payload")]
		query := "SELECT " + cvss.BucketSQL(alias) + " FROM (SELECT $1::jsonb AS payload) AS " + table
		for _, tc := range cases {
			var got string
			if err := db.QueryRowContext(ctx, query, tc.payload).Scan(&got); err != nil {
				t.Fatalf("%s %s: %v", alias, tc.name, err)
			}
			want := cvss.DeriveSeverity(tc.score, "", tc.label).Bucket
			if got != want {
				t.Errorf("%s %s: SQL bucket = %q, Go bucket = %q", alias, tc.name, got, want)
			}
		}
	}
}
