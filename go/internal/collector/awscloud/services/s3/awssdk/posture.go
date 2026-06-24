// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	s3service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3"
)

func (c *Client) getBucketWebsite(ctx context.Context, name string) (s3service.Website, error) {
	var output *awss3.GetBucketWebsiteOutput
	err := c.recordAPICall(ctx, "GetBucketWebsite", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketWebsite(callCtx, &awss3.GetBucketWebsiteInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "NoSuchWebsiteConfiguration") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil {
		return s3service.Website{}, err
	}
	return s3service.Website{
		Enabled:               true,
		HasIndexDocument:      output.IndexDocument != nil,
		HasErrorDocument:      output.ErrorDocument != nil,
		RedirectAllRequestsTo: redirectHost(output.RedirectAllRequestsTo),
		RoutingRuleCount:      len(output.RoutingRules),
	}, nil
}

func (c *Client) getBucketLogging(ctx context.Context, name string) (s3service.Logging, error) {
	var output *awss3.GetBucketLoggingOutput
	err := c.recordAPICall(ctx, "GetBucketLogging", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketLogging(callCtx, &awss3.GetBucketLoggingInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		return err
	})
	if err != nil || output == nil || output.LoggingEnabled == nil {
		return s3service.Logging{}, err
	}
	return s3service.Logging{
		Enabled:      true,
		TargetBucket: aws.ToString(output.LoggingEnabled.TargetBucket),
		TargetPrefix: aws.ToString(output.LoggingEnabled.TargetPrefix),
	}, nil
}

// getBucketReplication reports only whether a replication configuration with at
// least one rule is present. Destination buckets, filters, and replica KMS keys
// are intentionally not read into scanner-owned metadata.
func (c *Client) getBucketReplication(ctx context.Context, name string) (s3service.Replication, error) {
	var output *awss3.GetBucketReplicationOutput
	err := c.recordAPICall(ctx, "GetBucketReplication", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketReplication(callCtx, &awss3.GetBucketReplicationInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "ReplicationConfigurationNotFoundError") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil || output.ReplicationConfiguration == nil {
		return s3service.Replication{}, err
	}
	return s3service.Replication{
		Enabled: len(output.ReplicationConfiguration.Rules) > 0,
	}, nil
}

// bucketPolicyMetadata is the bounded, metadata-only projection the S3 adapter
// derives from a transient bucket-policy parse. It carries the present flag, the
// posture booleans, the external-principal grants, and the normalized
// resource-policy permission statements. The raw policy document, statement
// bodies, and condition values are never represented here.
type bucketPolicyMetadata struct {
	present          bool
	public           *bool
	crossAccount     *bool
	grants           []s3service.ExternalPrincipalGrant
	policyStatements []s3service.ResourcePolicyStatement
}

// getBucketPolicyMetadata reads the bucket policy document, derives posture
// booleans, bounded external-principal metadata, and normalized resource-policy
// permission statements from it, and discards the raw document. The raw policy
// JSON never leaves this method: only derived booleans, the present flag,
// metadata-only principal observations, and normalized statement summaries (no
// statement body, no condition values) are returned. A missing policy is
// reported as present=false with nil booleans and no statements. A malformed
// policy is surfaced to the caller rather than silently emitting a wrong
// posture.
func (c *Client) getBucketPolicyMetadata(ctx context.Context, name string) (bucketPolicyMetadata, error) {
	var output *awss3.GetBucketPolicyOutput
	callErr := c.recordAPICall(ctx, "GetBucketPolicy", func(callCtx context.Context) error {
		var getErr error
		output, getErr = c.client.GetBucketPolicy(callCtx, &awss3.GetBucketPolicyInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(getErr, "NoSuchBucketPolicy") {
			output = nil
			return nil
		}
		return getErr
	})
	if callErr != nil {
		return bucketPolicyMetadata{}, callErr
	}
	if output == nil || aws.ToString(output.Policy) == "" {
		return bucketPolicyMetadata{}, nil
	}
	document := aws.ToString(output.Policy)
	policyDocument, err := decodeBucketPolicyDocument(document)
	if err != nil {
		return bucketPolicyMetadata{}, err
	}
	public, crossAccount := bucketPolicyFlagsFromDocument(policyDocument, c.boundary.AccountID)
	derivedGrants := bucketPolicyExternalPrincipalGrantsFromDocument(policyDocument, c.boundary.AccountID)
	statements := bucketPolicyResourcePermissionStatementsFromDocument(policyDocument, c.boundary.AccountID)
	return bucketPolicyMetadata{
		present:          true,
		public:           public,
		crossAccount:     crossAccount,
		grants:           externalPrincipalGrants(derivedGrants),
		policyStatements: statements,
	}, nil
}

func externalPrincipalGrants(grants []principalGrant) []s3service.ExternalPrincipalGrant {
	if len(grants) == 0 {
		return nil
	}
	output := make([]s3service.ExternalPrincipalGrant, 0, len(grants))
	for _, grant := range grants {
		output = append(output, s3service.ExternalPrincipalGrant{
			PrincipalKind:      grant.Kind,
			PrincipalValue:     grant.Value,
			PrincipalAccountID: grant.AccountID,
			PrincipalPartition: grant.Partition,
			PrincipalService:   grant.Service,
			GrantOutcome:       grant.Outcome,
			Public:             grant.Public,
			CrossAccount:       grant.CrossAccount,
			ServicePrincipal:   grant.ServicePrincipal,
			Unsupported:        grant.Unsupported,
			UnsupportedKey:     grant.UnsupportedKey,
			SourceStatementID:  grant.StatementSID,
		})
	}
	return output
}

func redirectHost(redirect *awss3types.RedirectAllRequestsTo) string {
	if redirect == nil {
		return ""
	}
	return aws.ToString(redirect.HostName)
}
