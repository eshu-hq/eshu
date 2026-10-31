// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cvss

import (
	"math"
	"strings"
)

// Severity buckets, from most to least severe. BucketNone means no usable
// severity evidence, never "not vulnerable".
const (
	BucketCritical = "critical"
	BucketHigh     = "high"
	BucketMedium   = "medium"
	BucketLow      = "low"
	BucketNone     = "none"
)

// Score sources recorded on a finding. A finding whose score was computed
// from a vector carries ScoreSourceDerivedFromVector so a reader never takes
// it for a number the source published. An absent source on a finding with a
// score means ScoreSourceReported.
const (
	ScoreSourceReported          = "source_reported"
	ScoreSourceDerivedFromVector = "derived_from_vector"
)

// bucketThreshold is one row of the qualitative severity rating scale: a score
// at or above min (or strictly above it when exclusive) belongs to bucket.
type bucketThreshold struct {
	min       float64
	exclusive bool
	bucket    string
}

// bucketThresholds is the CVSS v3.1 and v4.0 qualitative severity rating
// scale (https://www.first.org/cvss/v3.1/specification-document section 5),
// ordered from the highest bucket down. Bucket and BucketSQL both read it.
var bucketThresholds = []bucketThreshold{
	{min: 9.0, bucket: BucketCritical},
	{min: 7.0, bucket: BucketHigh},
	{min: 4.0, bucket: BucketMedium},
	{min: 0.0, exclusive: true, bucket: BucketLow},
}

// labelBuckets maps a normalized (trimmed, lower-case) source severity label
// to its bucket. "moderate" is GitHub and OSV's name for medium and
// "important" is the Red Hat and Microsoft name for high. Labels outside this
// table, including "none" and "informational", map to BucketNone.
var labelBuckets = map[string]string{
	"critical":  BucketCritical,
	"high":      BucketHigh,
	"important": BucketHigh,
	"medium":    BucketMedium,
	"moderate":  BucketMedium,
	"low":       BucketLow,
}

// Bucket returns the severity bucket of a CVSS score: critical from 9.0, high
// from 7.0, medium from 4.0, low above 0, and none otherwise.
func Bucket(score float64) string {
	for _, row := range bucketThresholds {
		if score > row.min || (!row.exclusive && score == row.min) {
			return row.bucket
		}
	}
	return BucketNone
}

// LabelBucket returns the bucket of a source severity label, ignoring case
// and surrounding space. An empty or unrecognized label returns BucketNone.
func LabelBucket(label string) string {
	if bucket, ok := labelBuckets[strings.ToLower(strings.TrimSpace(label))]; ok {
		return bucket
	}
	return BucketNone
}

// Severity is the outcome of DeriveSeverity.
type Severity struct {
	// Score is the CVSS score the finding carries: the source-reported score,
	// the score computed from the vector, or 0 when neither exists.
	Score float64
	// ScoreSource is ScoreSourceReported or ScoreSourceDerivedFromVector for a
	// nonzero Score, and empty when Score is 0.
	ScoreSource string
	// Bucket is the severity bucket: from Score when it is nonzero, otherwise
	// from the label.
	Bucket string
}

// DeriveSeverity turns whatever severity evidence one source published into a
// score and a bucket, in a fixed order of precedence:
//
//  1. A source-reported score above zero is used as published.
//  2. Otherwise a valid CVSS vector yields a computed base score, marked
//     ScoreSourceDerivedFromVector. A malformed or unsupported vector yields
//     nothing, and a vector that scores 0 (no impact) defers to the label.
//  3. Otherwise the label sets the bucket only. A label never invents a
//     score, because a band is not a number.
//  4. Otherwise the bucket is BucketNone.
//
// The reported score outranks the vector because it is the source's own
// number, which may include a rounding or an adjustment the vector alone does
// not reproduce. The same table drives the SQL bucket (BucketSQL), so the
// stored and the read-time bucket agree.
func DeriveSeverity(reportedScore float64, vector, label string) Severity {
	if reportedScore > 0 && !math.IsInf(reportedScore, 0) {
		return Severity{Score: reportedScore, ScoreSource: ScoreSourceReported, Bucket: Bucket(reportedScore)}
	}
	if strings.TrimSpace(vector) != "" {
		if result, err := BaseScore(vector); err == nil && result.Score > 0 {
			return Severity{Score: result.Score, ScoreSource: ScoreSourceDerivedFromVector, Bucket: Bucket(result.Score)}
		}
	}
	return Severity{Bucket: LabelBucket(label)}
}
