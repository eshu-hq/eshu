// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsreviewer "github.com/aws/aws-sdk-go-v2/service/codegurureviewer"
	reviewertypes "github.com/aws/aws-sdk-go-v2/service/codegurureviewer/types"

	codeguruservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codeguru"
)

// listRepositoryAssociations pages every CodeGuru Reviewer repository
// association to exhaustion and enriches each with the describe-only KMS,
// S3-backing, and tag metadata the summary does not carry.
func (c *Client) listRepositoryAssociations(ctx context.Context) ([]codeguruservice.RepositoryAssociation, error) {
	var associations []codeguruservice.RepositoryAssociation
	var nextToken *string
	for {
		var page *awsreviewer.ListRepositoryAssociationsOutput
		err := c.recordAPICall(ctx, "ListRepositoryAssociations", func(callCtx context.Context) error {
			var err error
			page, err = c.reviewer.ListRepositoryAssociations(callCtx, &awsreviewer.ListRepositoryAssociationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return associations, nil
		}
		for _, summary := range page.RepositoryAssociationSummaries {
			mapped, err := c.mapAssociation(ctx, summary)
			if err != nil {
				return nil, err
			}
			associations = append(associations, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return associations, nil
		}
	}
}

// mapAssociation maps a list summary into the scanner-owned association,
// enriching it with the describe-only KMS key, S3 backing bucket, and tag
// metadata. The summary carries identity, owner, provider, and state; the
// describe call adds the customer-managed encryption key and S3 backing
// reference, neither of which is code-review or recommendation content.
func (c *Client) mapAssociation(
	ctx context.Context,
	summary reviewertypes.RepositoryAssociationSummary,
) (codeguruservice.RepositoryAssociation, error) {
	mapped := codeguruservice.RepositoryAssociation{
		ARN:           strings.TrimSpace(aws.ToString(summary.AssociationArn)),
		AssociationID: strings.TrimSpace(aws.ToString(summary.AssociationId)),
		Name:          strings.TrimSpace(aws.ToString(summary.Name)),
		Owner:         strings.TrimSpace(aws.ToString(summary.Owner)),
		ProviderType:  strings.TrimSpace(string(summary.ProviderType)),
		State:         strings.TrimSpace(string(summary.State)),
		ConnectionARN: strings.TrimSpace(aws.ToString(summary.ConnectionArn)),
		LastUpdatedAt: aws.ToTime(summary.LastUpdatedTimeStamp),
	}
	if err := c.describeAssociation(ctx, &mapped); err != nil {
		return codeguruservice.RepositoryAssociation{}, err
	}
	return mapped, nil
}

// describeAssociation enriches mapped with the describe-only metadata (KMS key,
// S3 bucket name, encryption option, created-at, owner/connection fallbacks) and
// resource tags. It keys the describe on the association ARN; when no ARN is
// present (defensive) it leaves the summary fields in place.
func (c *Client) describeAssociation(
	ctx context.Context,
	mapped *codeguruservice.RepositoryAssociation,
) error {
	arn := strings.TrimSpace(mapped.ARN)
	if arn == "" {
		return nil
	}
	var output *awsreviewer.DescribeRepositoryAssociationOutput
	err := c.recordAPICall(ctx, "DescribeRepositoryAssociation", func(callCtx context.Context) error {
		var err error
		output, err = c.reviewer.DescribeRepositoryAssociation(callCtx, &awsreviewer.DescribeRepositoryAssociationInput{
			AssociationArn: aws.String(arn),
		})
		return err
	})
	if err != nil || output == nil {
		return err
	}
	applyAssociationDetail(mapped, output.RepositoryAssociation)
	mapped.Tags = trimTags(output.Tags)
	return nil
}

// applyAssociationDetail copies the describe-only fields onto mapped. It records
// only the metadata references (S3 bucket name, customer-managed KMS key id,
// encryption option) and never the analyzed source object keys or code body.
func applyAssociationDetail(
	mapped *codeguruservice.RepositoryAssociation,
	detail *reviewertypes.RepositoryAssociation,
) {
	if detail == nil {
		return
	}
	if owner := strings.TrimSpace(aws.ToString(detail.Owner)); owner != "" {
		mapped.Owner = owner
	}
	if provider := strings.TrimSpace(string(detail.ProviderType)); provider != "" {
		mapped.ProviderType = provider
	}
	if connection := strings.TrimSpace(aws.ToString(detail.ConnectionArn)); connection != "" {
		mapped.ConnectionARN = connection
	}
	if state := strings.TrimSpace(string(detail.State)); state != "" {
		mapped.State = state
	}
	mapped.CreatedAt = aws.ToTime(detail.CreatedTimeStamp)
	if updated := aws.ToTime(detail.LastUpdatedTimeStamp); !updated.IsZero() {
		mapped.LastUpdatedAt = updated
	}
	if details := detail.KMSKeyDetails; details != nil {
		mapped.KMSKeyID = strings.TrimSpace(aws.ToString(details.KMSKeyId))
		mapped.EncryptionOption = strings.TrimSpace(string(details.EncryptionOption))
	}
	if s3 := detail.S3RepositoryDetails; s3 != nil {
		mapped.S3BucketName = strings.TrimSpace(aws.ToString(s3.BucketName))
	}
}

// trimTags returns a trimmed-key copy of the AWS tag map, or nil when empty.
func trimTags(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	tags := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		tags[trimmed] = value
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}
