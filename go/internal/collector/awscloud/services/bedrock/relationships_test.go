// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestNormalizeS3BucketARNRejectsEmptyBucket proves the S3 bucket ARN
// synthesizer never produces an invalid `arn:<partition>:s3:::` with an empty
// bucket segment. A non-ARN input that carries no bucket name (an `s3://` or
// `s3:///path` scheme prefix with nothing addressable after it) must be reported
// as unresolvable so the caller drops the relationship rather than emitting an
// edge with an empty join key.
func TestNormalizeS3BucketARNRejectsEmptyBucket(t *testing.T) {
	for _, tc := range []struct {
		name      string
		partition string
		bucketArg string
		wantARN   string
		wantOK    bool
	}{
		{
			name:      "already an ARN passes through",
			partition: "aws",
			bucketArg: "arn:aws:s3:::kb-docs",
			wantARN:   "arn:aws:s3:::kb-docs",
			wantOK:    true,
		},
		{
			name:      "bare bucket name synthesizes an ARN",
			partition: "aws",
			bucketArg: "kb-docs",
			wantARN:   "arn:aws:s3:::kb-docs",
			wantOK:    true,
		},
		{
			name:      "s3 url with bucket and key keeps the bucket",
			partition: "aws",
			bucketArg: "s3://bucket/key",
			wantARN:   "arn:aws:s3:::bucket",
			wantOK:    true,
		},
		{
			name:      "scheme-only s3 url is unresolvable",
			partition: "aws",
			bucketArg: "s3://",
			wantARN:   "",
			wantOK:    false,
		},
		{
			name:      "s3 url with empty bucket and a path is unresolvable",
			partition: "aws",
			bucketArg: "s3:///path",
			wantARN:   "",
			wantOK:    false,
		},
		{
			name:      "partition is preserved for synthesized GovCloud ARNs",
			partition: "aws-us-gov",
			bucketArg: "gov-bucket",
			wantARN:   "arn:aws-us-gov:s3:::gov-bucket",
			wantOK:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotARN, gotOK := normalizeS3BucketARN(tc.partition, tc.bucketArg)
			if gotOK != tc.wantOK {
				t.Fatalf("normalizeS3BucketARN(%q, %q) ok = %v, want %v", tc.partition, tc.bucketArg, gotOK, tc.wantOK)
			}
			if gotARN != tc.wantARN {
				t.Fatalf("normalizeS3BucketARN(%q, %q) = %q, want %q", tc.partition, tc.bucketArg, gotARN, tc.wantARN)
			}
		})
	}
}

// TestKnowledgeBaseS3SourceWithEmptyBucketEmitsNoRelationship proves the caller
// drops the S3 data-source relationship when the reported bucket reference
// cannot be resolved to a non-empty bucket. An emitted edge with an empty join
// key would silently break the graph join, so no edge is the correct truth.
func TestKnowledgeBaseS3SourceWithEmptyBucketEmitsNoRelationship(t *testing.T) {
	kb := KnowledgeBase{
		ARN:  "arn:aws:bedrock:us-east-1:123456789012:knowledge-base/KB1",
		ID:   "KB1",
		Name: "docs-kb",
		DataSources: []KnowledgeBaseDataSource{
			{ID: "DS-BAD", Name: "bad-s3", Type: "S3", S3BucketARN: "s3://"},
			{ID: "DS-BAD2", Name: "bad-s3-path", Type: "S3", S3BucketARN: "s3:///path"},
			{ID: "DS-GOOD", Name: "good-s3", Type: "S3", S3BucketARN: "s3://kb-docs/key"},
		},
	}
	observations := knowledgeBaseRelationships(kb)

	var s3Targets []string
	for _, obs := range observations {
		if obs.RelationshipType == awscloud.RelationshipBedrockKnowledgeBaseUsesS3DataSource {
			s3Targets = append(s3Targets, obs.TargetResourceID)
		}
	}
	if len(s3Targets) != 1 {
		t.Fatalf("S3 data-source relationships = %v, want exactly the resolvable bucket", s3Targets)
	}
	if got, want := s3Targets[0], "arn:aws:s3:::kb-docs"; got != want {
		t.Fatalf("resolved S3 target = %q, want %q", got, want)
	}
	for _, obs := range observations {
		if got, _ := obs.TargetResourceID, obs.TargetType; got == "arn:aws:s3:::" {
			t.Fatalf("emitted relationship with invalid empty-bucket ARN %q", obs.TargetResourceID)
		}
	}
}
