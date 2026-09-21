// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func (f *fakeS3API) GetBucketReplication(
	context.Context,
	*awss3.GetBucketReplicationInput,
	...func(*awss3.Options),
) (*awss3.GetBucketReplicationOutput, error) {
	if f.replicationErr != nil {
		return nil, f.replicationErr
	}
	return f.replication, nil
}

func (f *fakeS3API) GetBucketPolicy(
	context.Context,
	*awss3.GetBucketPolicyInput,
	...func(*awss3.Options),
) (*awss3.GetBucketPolicyOutput, error) {
	if f.policyErr != nil {
		return nil, f.policyErr
	}
	return f.policy, nil
}

// TestIsOptionalMissingS3ConfigRecognizesPostureCodes proves the new posture
// reads treat a missing replication configuration or absent bucket policy as
// empty posture, not a scan failure.
func TestIsOptionalMissingS3ConfigRecognizesPostureCodes(t *testing.T) {
	for _, code := range []string{
		"ReplicationConfigurationNotFoundError",
		"NoSuchBucketPolicy",
	} {
		if !isOptionalMissingS3Config(apiError(code), code) {
			t.Fatalf("isOptionalMissingS3Config(%q) = false, want true", code)
		}
	}
}
