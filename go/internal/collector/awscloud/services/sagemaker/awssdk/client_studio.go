// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssagemaker "github.com/aws/aws-sdk-go-v2/service/sagemaker"

	smservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sagemaker"
)

// ListDomains returns Studio domain metadata with VPC placement read from
// DescribeDomain.
func (c *Client) ListDomains(ctx context.Context) ([]smservice.Domain, error) {
	paginator := awssagemaker.NewListDomainsPaginator(c.client, &awssagemaker.ListDomainsInput{})
	var domains []smservice.Domain
	for paginator.HasMorePages() {
		var page *awssagemaker.ListDomainsOutput
		if err := c.page(ctx, "ListDomains", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.Domains {
			domain := smservice.Domain{
				ARN:              aws.ToString(summary.DomainArn),
				ID:               aws.ToString(summary.DomainId),
				Name:             aws.ToString(summary.DomainName),
				Status:           string(summary.Status),
				URL:              aws.ToString(summary.Url),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
			}
			if err := c.enrichDomain(ctx, &domain); err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, domain.ARN)
			if err != nil {
				return nil, err
			}
			domain.Tags = tags
			domains = append(domains, domain)
		}
	}
	return domains, nil
}

func (c *Client) enrichDomain(ctx context.Context, domain *smservice.Domain) error {
	id := strings.TrimSpace(domain.ID)
	if id == "" {
		return nil
	}
	var output *awssagemaker.DescribeDomainOutput
	if err := c.page(ctx, "DescribeDomain", func(callCtx context.Context) (err error) {
		output, err = c.client.DescribeDomain(callCtx, &awssagemaker.DescribeDomainInput{
			DomainId: aws.String(id),
		})
		return err
	}); err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	domain.VPCID = aws.ToString(output.VpcId)
	domain.AuthMode = string(output.AuthMode)
	domain.SubnetIDs = append([]string(nil), output.SubnetIds...)
	return nil
}

// ListUserProfiles returns Studio user-profile metadata. The parent domain id
// comes from the list summary, so the user-profile-to-domain relationship needs
// no Describe call.
func (c *Client) ListUserProfiles(ctx context.Context) ([]smservice.UserProfile, error) {
	paginator := awssagemaker.NewListUserProfilesPaginator(c.client, &awssagemaker.ListUserProfilesInput{})
	var profiles []smservice.UserProfile
	for paginator.HasMorePages() {
		var page *awssagemaker.ListUserProfilesOutput
		if err := c.page(ctx, "ListUserProfiles", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.UserProfiles {
			profiles = append(profiles, smservice.UserProfile{
				Name:             aws.ToString(summary.UserProfileName),
				DomainID:         aws.ToString(summary.DomainId),
				Status:           string(summary.Status),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
			})
		}
	}
	return profiles, nil
}

// ListApps returns Studio app metadata from the list summary.
func (c *Client) ListApps(ctx context.Context) ([]smservice.App, error) {
	paginator := awssagemaker.NewListAppsPaginator(c.client, &awssagemaker.ListAppsInput{})
	var apps []smservice.App
	for paginator.HasMorePages() {
		var page *awssagemaker.ListAppsOutput
		if err := c.page(ctx, "ListApps", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.Apps {
			apps = append(apps, smservice.App{
				Name:         aws.ToString(summary.AppName),
				Type:         string(summary.AppType),
				DomainID:     aws.ToString(summary.DomainId),
				UserProfile:  aws.ToString(summary.UserProfileName),
				SpaceName:    aws.ToString(summary.SpaceName),
				Status:       string(summary.Status),
				CreationTime: aws.ToTime(summary.CreationTime),
			})
		}
	}
	return apps, nil
}

// ListInferenceComponents returns inference-component metadata from the list
// summary, including the hosting endpoint identity.
func (c *Client) ListInferenceComponents(ctx context.Context) ([]smservice.InferenceComponent, error) {
	paginator := awssagemaker.NewListInferenceComponentsPaginator(c.client, &awssagemaker.ListInferenceComponentsInput{})
	var components []smservice.InferenceComponent
	for paginator.HasMorePages() {
		var page *awssagemaker.ListInferenceComponentsOutput
		if err := c.page(ctx, "ListInferenceComponents", func(callCtx context.Context) (err error) {
			page, err = paginator.NextPage(callCtx)
			return err
		}); err != nil {
			return nil, err
		}
		for _, summary := range page.InferenceComponents {
			components = append(components, smservice.InferenceComponent{
				ARN:              aws.ToString(summary.InferenceComponentArn),
				Name:             aws.ToString(summary.InferenceComponentName),
				Status:           string(summary.InferenceComponentStatus),
				EndpointName:     aws.ToString(summary.EndpointName),
				EndpointARN:      aws.ToString(summary.EndpointArn),
				VariantName:      aws.ToString(summary.VariantName),
				CreationTime:     aws.ToTime(summary.CreationTime),
				LastModifiedTime: aws.ToTime(summary.LastModifiedTime),
			})
		}
	}
	return components, nil
}

// Interface assertion: the SDK adapter satisfies the scanner-owned Client
// contract. This keeps the read surface honest at compile time.
var _ smservice.Client = (*Client)(nil)
