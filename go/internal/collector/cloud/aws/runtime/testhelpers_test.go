// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime_test

import (
	"github.com/aws/aws-sdk-go-v2/aws"
)

// staticAWSConfigLease is a test double satisfying awsruntime.AWSConfigLease
// for builders that need a non-nil config but no lease semantics.
type staticAWSConfigLease struct {
	config aws.Config
}

// AWSConfig returns the canned aws.Config.
func (l staticAWSConfigLease) AWSConfig() aws.Config {
	return l.config
}

// Release is a no-op for tests; production leases must clear credential
// material.
func (l staticAWSConfigLease) Release() error {
	return nil
}

// releaseOnlyLease satisfies awsruntime.CredentialLease but NOT AWSConfigLease
// so the runtime error path that requires an AWS-shaped lease has coverage.
type releaseOnlyLease struct{}

// Release is a no-op for tests.
func (l releaseOnlyLease) Release() error {
	return nil
}
