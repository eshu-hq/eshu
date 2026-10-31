// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"sort"
	"strconv"
	"strings"
)

// BucketSQL returns the SQL CASE expression that computes the severity bucket
// of one finding payload, so Postgres buckets exactly as DeriveSeverity does.
// payload is the jsonb column expression of the finding fact, for example
// "fact.payload" or "source.payload".
//
// The expression reads the finding's stored cvss_score first, which holds
// either the source-reported or the vector-derived score. Only when that score
// is zero or absent does it fall back to the finding's
// provenance.selected_severity_label, mapped through the same label table as
// LabelBucket. Both the winner recompute and the read-time findings queries
// call this function, so the stored severity_bucket and the live filter cannot
// disagree.
func BucketSQL(payload string) string {
	score := "COALESCE(NULLIF(" + payload + "->>'cvss_score', '')::numeric, 0)"
	label := "lower(btrim(" + payload + " #>> '{provenance,selected_severity_label}'))"

	var sql strings.Builder
	sql.WriteString("CASE\n")
	for _, row := range bucketThresholds {
		op := ">="
		if row.exclusive {
			op = ">"
		}
		sql.WriteString("    WHEN " + score + " " + op + " " + formatThreshold(row.min) + " THEN '" + row.bucket + "'\n")
	}
	// The label arms are a simple CASE so Postgres evaluates the JSONB path
	// extraction once per row, not once per arm.
	sql.WriteString("    ELSE CASE " + label + "\n")
	for _, arm := range labelArms() {
		sql.WriteString("        WHEN '" + arm.label + "' THEN '" + arm.bucket + "'\n")
	}
	sql.WriteString("        ELSE '" + BucketNone + "'\n    END\n  END")
	return sql.String()
}

// labelArm is one label to bucket mapping rendered into the SQL.
type labelArm struct {
	label  string
	bucket string
}

// labelArms renders the label table in bucket severity order with labels
// sorted inside each bucket, so the SQL is deterministic.
func labelArms() []labelArm {
	byBucket := make(map[string][]string, len(labelBuckets))
	for label, bucket := range labelBuckets {
		byBucket[bucket] = append(byBucket[bucket], label)
	}
	var arms []labelArm
	for _, bucket := range []string{BucketCritical, BucketHigh, BucketMedium, BucketLow} {
		labels := byBucket[bucket]
		sort.Strings(labels)
		for _, label := range labels {
			arms = append(arms, labelArm{label: label, bucket: bucket})
		}
	}
	return arms
}

// formatThreshold renders a threshold with one decimal place.
func formatThreshold(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}
