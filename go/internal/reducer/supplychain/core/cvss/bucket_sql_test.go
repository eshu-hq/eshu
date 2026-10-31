// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"sort"
	"strings"
	"testing"
)

func TestBucketSQLIsDeterministicAndAliasScoped(t *testing.T) {
	t.Parallel()

	first := BucketSQL("fact.payload")
	if second := BucketSQL("fact.payload"); first != second {
		t.Fatalf("BucketSQL is not deterministic:\n%s\n---\n%s", first, second)
	}
	other := BucketSQL("source.payload")
	if strings.Contains(other, "fact.payload") {
		t.Fatalf("BucketSQL(source.payload) leaked the fact alias:\n%s", other)
	}
	if strings.ReplaceAll(other, "source.payload", "fact.payload") != first {
		t.Fatalf("the two aliases must produce byte-identical SQL after substitution:\n%s\n---\n%s", first, other)
	}
}

// TestBucketSQLCarriesEveryTableRow pins the SQL to the Go tables: every
// threshold and every label the Go side maps must appear in the SQL, and the
// SQL must map no label the Go side does not.
func TestBucketSQLCarriesEveryTableRow(t *testing.T) {
	t.Parallel()

	sql := BucketSQL("fact.payload")
	for _, row := range bucketThresholds {
		op := ">="
		if row.exclusive {
			op = ">"
		}
		want := op + " " + formatThreshold(row.min) + " THEN '" + row.bucket + "'"
		if !strings.Contains(sql, want) {
			t.Errorf("BucketSQL is missing the threshold %q:\n%s", want, sql)
		}
	}
	labels := make([]string, 0, len(labelBuckets))
	for label := range labelBuckets {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		if want := "WHEN '" + label + "' THEN '" + labelBuckets[label] + "'"; !strings.Contains(sql, want) {
			t.Errorf("BucketSQL does not map the label %q to %q (want %q)", label, labelBuckets[label], want)
		}
	}
	// A label the Go table does not know must not appear as a quoted literal
	// next to the label expression.
	for _, stray := range []string{"'informational'", "'unknown'", "'severe'"} {
		if strings.Contains(sql, stray) {
			t.Errorf("BucketSQL maps %s, which LabelBucket sends to none", stray)
		}
	}
	if strings.Count(sql, "ELSE 'none'") != 1 || !strings.Contains(sql, "        ELSE 'none'") {
		t.Errorf("BucketSQL must end the label CASE with a single ELSE 'none':\n%s", sql)
	}
	if strings.Count(sql, "selected_severity_label") != 1 {
		t.Errorf("BucketSQL must extract the label once so Postgres evaluates it once per row:\n%s", sql)
	}
}

// TestBucketSQLReadsScoreBeforeLabel pins the precedence the Go side uses: a
// positive score decides the bucket, the label only applies when the score
// is zero or absent.
func TestBucketSQLReadsScoreBeforeLabel(t *testing.T) {
	t.Parallel()

	sql := BucketSQL("fact.payload")
	lastScore := strings.LastIndex(sql, "'cvss_score'")
	firstLabel := strings.Index(sql, "selected_severity_label")
	if lastScore < 0 || firstLabel < 0 {
		t.Fatalf("BucketSQL must read cvss_score and selected_severity_label:\n%s", sql)
	}
	if lastScore > firstLabel {
		t.Fatalf("a label arm precedes a score arm:\n%s", sql)
	}
}

// TestBucketSQLGolden pins the exact SQL text, written out here by hand and not
// built from the package's tables. It is the CI guard for the expression
// itself: the score cast, the label extraction path, the lower() and btrim()
// normalization, and every label arm. The execution proof against a real
// Postgres (TestSeverityBucketSQLMatchesGoBucketOnRealPostgres in
// go/internal/storage/postgres) runs only where ESHU_POSTGRES_TEST_DSN is set,
// so without this test a broken label path would pass every always-on test. When
// the label or threshold table changes on purpose, update this literal and the
// docs together.
func TestBucketSQLGolden(t *testing.T) {
	t.Parallel()

	const want = `CASE
    WHEN COALESCE(NULLIF(x.payload->>'cvss_score', '')::numeric, 0) >= 9.0 THEN 'critical'
    WHEN COALESCE(NULLIF(x.payload->>'cvss_score', '')::numeric, 0) >= 7.0 THEN 'high'
    WHEN COALESCE(NULLIF(x.payload->>'cvss_score', '')::numeric, 0) >= 4.0 THEN 'medium'
    WHEN COALESCE(NULLIF(x.payload->>'cvss_score', '')::numeric, 0) > 0.0 THEN 'low'
    ELSE CASE lower(btrim(x.payload #>> '{provenance,selected_severity_label}'))
        WHEN 'critical' THEN 'critical'
        WHEN 'high' THEN 'high'
        WHEN 'important' THEN 'high'
        WHEN 'medium' THEN 'medium'
        WHEN 'moderate' THEN 'medium'
        WHEN 'low' THEN 'low'
        ELSE 'none'
    END
  END`
	if got := BucketSQL("x.payload"); got != want {
		t.Fatalf("BucketSQL text changed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
