// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslicensemanager "github.com/aws/aws-sdk-go-v2/service/licensemanager"
	awslicensemanagertypes "github.com/aws/aws-sdk-go-v2/service/licensemanager/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	licensemanagerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/licensemanager"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS License Manager API the
// adapter calls. It is deliberately limited to the license-configuration list
// read, the per-configuration association list read, and the resource-tag read.
// It exposes no GetLicense, no CheckoutLicense / CheckInLicense, no
// GetAccessToken or entitlement read, and no Create/Update/Delete mutation, so
// the adapter cannot grant, check out, mutate, or read the entitlement token or
// usage records of any license. The exclusion_test reflects over this interface
// to enforce that contract at build time.
type apiClient interface {
	ListLicenseConfigurations(
		context.Context,
		*awslicensemanager.ListLicenseConfigurationsInput,
		...func(*awslicensemanager.Options),
	) (*awslicensemanager.ListLicenseConfigurationsOutput, error)
	ListAssociationsForLicenseConfiguration(
		context.Context,
		*awslicensemanager.ListAssociationsForLicenseConfigurationInput,
		...func(*awslicensemanager.Options),
	) (*awslicensemanager.ListAssociationsForLicenseConfigurationOutput, error)
	ListTagsForResource(
		context.Context,
		*awslicensemanager.ListTagsForResourceInput,
		...func(*awslicensemanager.Options),
	) (*awslicensemanager.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK License Manager control-plane calls into scanner-owned
// metadata. It never grants, checks out, or mutates a license, never reads an
// entitlement token, and never calls a checkout or mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a License Manager SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awslicensemanager.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns License Manager license-configuration metadata and the
// resource associations under each configuration visible to the configured AWS
// credentials. Entitlement tokens and usage records are never read.
func (c *Client) Snapshot(ctx context.Context) (licensemanagerservice.Snapshot, error) {
	configurations, err := c.listConfigurations(ctx)
	if err != nil {
		return licensemanagerservice.Snapshot{}, err
	}
	for i := range configurations {
		associations, err := c.listAssociations(ctx, configurations[i].ARN)
		if err != nil {
			return licensemanagerservice.Snapshot{}, err
		}
		configurations[i].Associations = associations
	}
	return licensemanagerservice.Snapshot{Configurations: configurations}, nil
}

func (c *Client) listConfigurations(ctx context.Context) ([]licensemanagerservice.Configuration, error) {
	var configurations []licensemanagerservice.Configuration
	var nextToken *string
	for {
		var page *awslicensemanager.ListLicenseConfigurationsOutput
		err := c.recordAPICall(ctx, "ListLicenseConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListLicenseConfigurations(callCtx, &awslicensemanager.ListLicenseConfigurationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return configurations, nil
		}
		for _, configuration := range page.LicenseConfigurations {
			mapped, err := c.mapConfiguration(ctx, configuration)
			if err != nil {
				return nil, err
			}
			configurations = append(configurations, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return configurations, nil
		}
	}
}

func (c *Client) mapConfiguration(
	ctx context.Context,
	configuration awslicensemanagertypes.LicenseConfiguration,
) (licensemanagerservice.Configuration, error) {
	arn := strings.TrimSpace(aws.ToString(configuration.LicenseConfigurationArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return licensemanagerservice.Configuration{}, err
	}
	mapped := licensemanagerservice.Configuration{
		ARN:                     arn,
		ID:                      strings.TrimSpace(aws.ToString(configuration.LicenseConfigurationId)),
		Name:                    strings.TrimSpace(aws.ToString(configuration.Name)),
		Description:             strings.TrimSpace(aws.ToString(configuration.Description)),
		Status:                  strings.TrimSpace(aws.ToString(configuration.Status)),
		LicenseCountingType:     strings.TrimSpace(string(configuration.LicenseCountingType)),
		LicenseCountHardLimit:   aws.ToBool(configuration.LicenseCountHardLimit),
		ConsumedLicenses:        aws.ToInt64(configuration.ConsumedLicenses),
		LicenseRuleCount:        len(configuration.LicenseRules),
		ProductInformationCount: len(configuration.ProductInformationList),
		OwnerAccountID:          strings.TrimSpace(aws.ToString(configuration.OwnerAccountId)),
		Tags:                    tags,
	}
	if configuration.LicenseCount != nil {
		mapped.LicenseCount = aws.ToInt64(configuration.LicenseCount)
		mapped.LicenseCountConfigured = true
	}
	if expiry := aws.ToInt64(configuration.LicenseExpiry); expiry > 0 {
		mapped.LicenseExpiry = unixSeconds(expiry)
	}
	return mapped, nil
}

func (c *Client) listAssociations(
	ctx context.Context,
	configurationARN string,
) ([]licensemanagerservice.Association, error) {
	configurationARN = strings.TrimSpace(configurationARN)
	if configurationARN == "" {
		return nil, nil
	}
	var associations []licensemanagerservice.Association
	var nextToken *string
	for {
		var page *awslicensemanager.ListAssociationsForLicenseConfigurationOutput
		err := c.recordAPICall(ctx, "ListAssociationsForLicenseConfiguration", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAssociationsForLicenseConfiguration(
				callCtx,
				&awslicensemanager.ListAssociationsForLicenseConfigurationInput{
					LicenseConfigurationArn: aws.String(configurationARN),
					NextToken:               nextToken,
				},
			)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return associations, nil
		}
		for _, association := range page.LicenseConfigurationAssociations {
			associations = append(associations, mapAssociation(association))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return associations, nil
		}
	}
}

func mapAssociation(
	association awslicensemanagertypes.LicenseConfigurationAssociation,
) licensemanagerservice.Association {
	return licensemanagerservice.Association{
		ResourceARN:     strings.TrimSpace(aws.ToString(association.ResourceArn)),
		ResourceType:    strings.TrimSpace(string(association.ResourceType)),
		ResourceOwnerID: strings.TrimSpace(aws.ToString(association.ResourceOwnerId)),
		AssociationTime: aws.ToTime(association.AssociationTime),
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awslicensemanager.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awslicensemanager.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException" ||
		code == "RateLimitExceededException"
}

var _ licensemanagerservice.Client = (*Client)(nil)

var _ apiClient = (*awslicensemanager.Client)(nil)
