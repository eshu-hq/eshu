// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import "testing"

func TestBucketThresholds(t *testing.T) {
	t.Parallel()

	// Thresholds are the CVSS v3.1 / v4.0 qualitative severity rating scale:
	// 0.1-3.9 low, 4.0-6.9 medium, 7.0-8.9 high, 9.0-10.0 critical
	// (https://www.first.org/cvss/v3.1/specification-document section 5).
	cases := []struct {
		score float64
		want  string
	}{
		{0, BucketNone},
		{-1, BucketNone},
		{0.1, BucketLow},
		{3.9, BucketLow},
		{4.0, BucketMedium},
		{6.9, BucketMedium},
		{7.0, BucketHigh},
		{8.9, BucketHigh},
		{9.0, BucketCritical},
		{10.0, BucketCritical},
	}
	for _, tc := range cases {
		if got := Bucket(tc.score); got != tc.want {
			t.Errorf("Bucket(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestLabelBucket(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"CRITICAL": BucketCritical, "critical": BucketCritical, " Critical ": BucketCritical,
		"HIGH": BucketHigh, "high": BucketHigh, "important": BucketHigh,
		"MODERATE": BucketMedium, "moderate": BucketMedium, "medium": BucketMedium,
		"LOW": BucketLow, "low": BucketLow,
		"": BucketNone, "none": BucketNone, "NONE": BucketNone, "unknown": BucketNone,
		"informational": BucketNone, "severe": BucketNone, "9.8": BucketNone,
	}
	for label, want := range cases {
		if got := LabelBucket(label); got != want {
			t.Errorf("LabelBucket(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestDeriveSeverityPrecedence(t *testing.T) {
	t.Parallel()

	const critical = "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" // 9.8
	const medium = "CVSS:3.1/AV:L/AC:L/PR:N/UI:R/S:U/C:L/I:N/A:N"   // 3.3, low
	cases := []struct {
		name       string
		score      float64
		vector     string
		label      string
		wantScore  float64
		wantSource string
		wantBucket string
	}{
		{"reported score wins over vector and label", 5.0, critical, "critical", 5.0, ScoreSourceReported, BucketMedium},
		{"reported score alone", 7.5, "", "", 7.5, ScoreSourceReported, BucketHigh},
		{"vector derives a score when none is reported", 0, critical, "low", 9.8, ScoreSourceDerivedFromVector, BucketCritical},
		{"vector only", 0, medium, "", 3.3, ScoreSourceDerivedFromVector, BucketLow},
		{"label only has a bucket and no score", 0, "", "MODERATE", 0, "", BucketMedium},
		{"malformed vector falls back to the label", 0, "CVSS:3.1/AV:N", "high", 0, "", BucketHigh},
		{"malformed vector without a label stays none", 0, "garbage", "", 0, "", BucketNone},
		{"unrecognized label stays none", 0, "", "severe", 0, "", BucketNone},
		{"nothing", 0, "", "", 0, "", BucketNone},
		{"zero-impact vector defers to the label", 0, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", "high", 0, "", BucketHigh},
		{"negative reported score is not a score", -1, critical, "", 9.8, ScoreSourceDerivedFromVector, BucketCritical},
	}
	for _, tc := range cases {
		got := DeriveSeverity(tc.score, tc.vector, tc.label)
		if got.Score != tc.wantScore || got.ScoreSource != tc.wantSource || got.Bucket != tc.wantBucket {
			t.Errorf("%s: DeriveSeverity(%v, %q, %q) = %+v, want score %v source %q bucket %q",
				tc.name, tc.score, tc.vector, tc.label, got, tc.wantScore, tc.wantSource, tc.wantBucket)
		}
	}
}
