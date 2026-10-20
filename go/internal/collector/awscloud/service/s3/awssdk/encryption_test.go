// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestGetBucketEncryptionSSES3AlgorithmProducesNoKMSKey proves that SSE-S3
// (AES256) encryption rules leave KMSMasterKeyID empty, confirming the
// derivation path for the algorithm that does not name a KMS key.
func TestGetBucketEncryptionSSES3AlgorithmProducesNoKMSKey(t *testing.T) {
	api := &fakeS3API{
		listBucketsPages: []*awss3.ListBucketsOutput{{
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("aes-bucket"),
				BucketRegion: aws.String("us-east-1"),
			}},
		}},
		encryption: &awss3.GetBucketEncryptionOutput{
			ServerSideEncryptionConfiguration: &awss3types.ServerSideEncryptionConfiguration{
				Rules: []awss3types.ServerSideEncryptionRule{{
					ApplyServerSideEncryptionByDefault: &awss3types.ServerSideEncryptionByDefault{
						SSEAlgorithm: awss3types.ServerSideEncryptionAes256,
						// KMSMasterKeyID intentionally absent for SSE-S3.
					},
					BucketKeyEnabled: aws.Bool(false),
				}},
			},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceS3,
		},
	}
	buckets, err := adapter.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets() error = %v, want nil", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("len(buckets) = %d, want 1", len(buckets))
	}
	rules := buckets[0].Encryption.Rules
	if len(rules) != 1 {
		t.Fatalf("encryption rule count = %d, want 1", len(rules))
	}
	if rules[0].Algorithm != "AES256" {
		t.Fatalf("Algorithm = %q, want AES256", rules[0].Algorithm)
	}
	if rules[0].KMSMasterKeyID != "" {
		t.Fatalf("KMSMasterKeyID = %q, want empty for SSE-S3", rules[0].KMSMasterKeyID)
	}
	if rules[0].BucketKey {
		t.Fatalf("BucketKey = true, want false for SSE-S3 rule with BucketKeyEnabled=false")
	}
}

// TestGetBucketEncryptionKMSAliasKeyIDPassedThrough proves that a
// KMSMasterKeyID supplied as a key alias (alias/...) rather than a full ARN is
// passed through verbatim. The client records only what the API returns; it does
// not canonicalize or resolve aliases.
func TestGetBucketEncryptionKMSAliasKeyIDPassedThrough(t *testing.T) {
	api := &fakeS3API{
		listBucketsPages: []*awss3.ListBucketsOutput{{
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("alias-bucket"),
				BucketRegion: aws.String("us-east-1"),
			}},
		}},
		encryption: &awss3.GetBucketEncryptionOutput{
			ServerSideEncryptionConfiguration: &awss3types.ServerSideEncryptionConfiguration{
				Rules: []awss3types.ServerSideEncryptionRule{{
					ApplyServerSideEncryptionByDefault: &awss3types.ServerSideEncryptionByDefault{
						SSEAlgorithm:   awss3types.ServerSideEncryptionAwsKms,
						KMSMasterKeyID: aws.String("alias/my-bucket-key"),
					},
					BucketKeyEnabled: aws.Bool(true),
				}},
			},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceS3,
		},
	}
	buckets, err := adapter.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets() error = %v, want nil", err)
	}
	rules := buckets[0].Encryption.Rules
	if len(rules) != 1 {
		t.Fatalf("encryption rule count = %d, want 1", len(rules))
	}
	if rules[0].KMSMasterKeyID != "alias/my-bucket-key" {
		t.Fatalf("KMSMasterKeyID = %q, want alias/my-bucket-key", rules[0].KMSMasterKeyID)
	}
	if !rules[0].BucketKey {
		t.Fatalf("BucketKey = false, want true")
	}
}

// TestGetBucketEncryptionMultipleRulesAllMapped proves that when the
// GetBucketEncryption response carries multiple rules, all are mapped: the first
// nil-ApplyServerSideEncryptionByDefault entry is silently skipped, and the
// remaining valid rules are emitted in order.
func TestGetBucketEncryptionMultipleRulesAllMapped(t *testing.T) {
	api := &fakeS3API{
		listBucketsPages: []*awss3.ListBucketsOutput{{
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("multi-rule-bucket"),
				BucketRegion: aws.String("us-east-1"),
			}},
		}},
		encryption: &awss3.GetBucketEncryptionOutput{
			ServerSideEncryptionConfiguration: &awss3types.ServerSideEncryptionConfiguration{
				Rules: []awss3types.ServerSideEncryptionRule{
					{
						// nil ApplyServerSideEncryptionByDefault must be skipped.
						ApplyServerSideEncryptionByDefault: nil,
					},
					{
						ApplyServerSideEncryptionByDefault: &awss3types.ServerSideEncryptionByDefault{
							SSEAlgorithm: awss3types.ServerSideEncryptionAes256,
						},
						BucketKeyEnabled: aws.Bool(false),
					},
					{
						ApplyServerSideEncryptionByDefault: &awss3types.ServerSideEncryptionByDefault{
							SSEAlgorithm:   awss3types.ServerSideEncryptionAwsKms,
							KMSMasterKeyID: aws.String("arn:aws:kms:us-east-1:123456789012:key/secondary"),
						},
						BucketKeyEnabled: aws.Bool(true),
					},
				},
			},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceS3,
		},
	}
	buckets, err := adapter.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets() error = %v, want nil", err)
	}
	rules := buckets[0].Encryption.Rules
	// The nil-default rule is skipped; the two valid rules must be present.
	if len(rules) != 2 {
		t.Fatalf("encryption rule count = %d, want 2 (nil rule skipped): %#v", len(rules), rules)
	}
	if rules[0].Algorithm != "AES256" {
		t.Fatalf("rules[0].Algorithm = %q, want AES256", rules[0].Algorithm)
	}
	if rules[1].Algorithm != "aws:kms" {
		t.Fatalf("rules[1].Algorithm = %q, want aws:kms", rules[1].Algorithm)
	}
	if rules[1].KMSMasterKeyID != "arn:aws:kms:us-east-1:123456789012:key/secondary" {
		t.Fatalf("rules[1].KMSMasterKeyID = %q, want full ARN", rules[1].KMSMasterKeyID)
	}
	if !rules[1].BucketKey {
		t.Fatalf("rules[1].BucketKey = false, want true")
	}
}
