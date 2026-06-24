// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappregistry "github.com/aws/aws-sdk-go-v2/service/servicecatalogappregistry"
	awsappregistrytypes "github.com/aws/aws-sdk-go-v2/service/servicecatalogappregistry/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	appregistryservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/servicecatalogappregistry"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Service Catalog AppRegistry
// API the adapter calls. It is deliberately limited to list reads for
// applications, attribute groups, an application's associated attribute groups
// and associated resources, plus resource-tag reads. It exposes no
// Get/Describe call that returns the attribute-group content body or an
// associated-resource tag value, and no Create/Update/Delete/Associate
// mutation, so the adapter cannot read content bodies or write AppRegistry
// state. The exclusion_test reflects over this interface to enforce that
// contract at build time.
type apiClient interface {
	ListApplications(
		context.Context,
		*awsappregistry.ListApplicationsInput,
		...func(*awsappregistry.Options),
	) (*awsappregistry.ListApplicationsOutput, error)
	ListAttributeGroups(
		context.Context,
		*awsappregistry.ListAttributeGroupsInput,
		...func(*awsappregistry.Options),
	) (*awsappregistry.ListAttributeGroupsOutput, error)
	ListAttributeGroupsForApplication(
		context.Context,
		*awsappregistry.ListAttributeGroupsForApplicationInput,
		...func(*awsappregistry.Options),
	) (*awsappregistry.ListAttributeGroupsForApplicationOutput, error)
	ListAssociatedResources(
		context.Context,
		*awsappregistry.ListAssociatedResourcesInput,
		...func(*awsappregistry.Options),
	) (*awsappregistry.ListAssociatedResourcesOutput, error)
	ListTagsForResource(
		context.Context,
		*awsappregistry.ListTagsForResourceInput,
		...func(*awsappregistry.Options),
	) (*awsappregistry.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Service Catalog AppRegistry control-plane calls into
// scanner-owned metadata. It never reads attribute-group content bodies or
// associated-resource tag values and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AppRegistry SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsappregistry.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns AppRegistry application and attribute-group metadata visible
// to the configured AWS credentials, with each application carrying its
// associated attribute groups and associated resources. Attribute-group content
// bodies and associated-resource tag values are never read.
func (c *Client) Snapshot(ctx context.Context) (appregistryservice.Snapshot, error) {
	attributeGroups, err := c.listAttributeGroups(ctx)
	if err != nil {
		return appregistryservice.Snapshot{}, err
	}
	applications, err := c.listApplications(ctx)
	if err != nil {
		return appregistryservice.Snapshot{}, err
	}
	for i := range applications {
		key := applicationKey(applications[i])
		groups, err := c.listApplicationAttributeGroups(ctx, key)
		if err != nil {
			return appregistryservice.Snapshot{}, err
		}
		applications[i].AttributeGroupARNs = groups
		resources, err := c.listAssociatedResources(ctx, key)
		if err != nil {
			return appregistryservice.Snapshot{}, err
		}
		applications[i].AssociatedResources = resources
	}
	return appregistryservice.Snapshot{
		Applications:    applications,
		AttributeGroups: attributeGroups,
	}, nil
}

// applicationKey returns the identifier the per-application list APIs accept
// (name or id). The id is preferred for stability; the name is the fallback.
func applicationKey(application appregistryservice.Application) string {
	if id := strings.TrimSpace(application.ID); id != "" {
		return id
	}
	return strings.TrimSpace(application.Name)
}

func (c *Client) listApplications(ctx context.Context) ([]appregistryservice.Application, error) {
	var applications []appregistryservice.Application
	var nextToken *string
	for {
		var page *awsappregistry.ListApplicationsOutput
		err := c.recordAPICall(ctx, "ListApplications", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListApplications(callCtx, &awsappregistry.ListApplicationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return applications, nil
		}
		for _, summary := range page.Applications {
			mapped, err := c.mapApplication(ctx, summary)
			if err != nil {
				return nil, err
			}
			applications = append(applications, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return applications, nil
		}
	}
}

func (c *Client) mapApplication(
	ctx context.Context,
	summary awsappregistrytypes.ApplicationSummary,
) (appregistryservice.Application, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return appregistryservice.Application{}, err
	}
	return appregistryservice.Application{
		ID:             strings.TrimSpace(aws.ToString(summary.Id)),
		ARN:            arn,
		Name:           strings.TrimSpace(aws.ToString(summary.Name)),
		Description:    strings.TrimSpace(aws.ToString(summary.Description)),
		CreationTime:   aws.ToTime(summary.CreationTime),
		LastUpdateTime: aws.ToTime(summary.LastUpdateTime),
		Tags:           tags,
	}, nil
}

func (c *Client) listAttributeGroups(ctx context.Context) ([]appregistryservice.AttributeGroup, error) {
	var groups []appregistryservice.AttributeGroup
	var nextToken *string
	for {
		var page *awsappregistry.ListAttributeGroupsOutput
		err := c.recordAPICall(ctx, "ListAttributeGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAttributeGroups(callCtx, &awsappregistry.ListAttributeGroupsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, summary := range page.AttributeGroups {
			mapped, err := c.mapAttributeGroup(ctx, summary)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return groups, nil
		}
	}
}

func (c *Client) mapAttributeGroup(
	ctx context.Context,
	summary awsappregistrytypes.AttributeGroupSummary,
) (appregistryservice.AttributeGroup, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return appregistryservice.AttributeGroup{}, err
	}
	return appregistryservice.AttributeGroup{
		ID:             strings.TrimSpace(aws.ToString(summary.Id)),
		ARN:            arn,
		Name:           strings.TrimSpace(aws.ToString(summary.Name)),
		Description:    strings.TrimSpace(aws.ToString(summary.Description)),
		CreationTime:   aws.ToTime(summary.CreationTime),
		LastUpdateTime: aws.ToTime(summary.LastUpdateTime),
		Tags:           tags,
	}, nil
}

func (c *Client) listApplicationAttributeGroups(ctx context.Context, application string) ([]string, error) {
	application = strings.TrimSpace(application)
	if application == "" {
		return nil, nil
	}
	var arns []string
	var nextToken *string
	for {
		var page *awsappregistry.ListAttributeGroupsForApplicationOutput
		err := c.recordAPICall(ctx, "ListAttributeGroupsForApplication", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAttributeGroupsForApplication(
				callCtx,
				&awsappregistry.ListAttributeGroupsForApplicationInput{
					Application: aws.String(application),
					NextToken:   nextToken,
				},
			)
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return arns, nil
		}
		for _, detail := range page.AttributeGroupsDetails {
			if arn := strings.TrimSpace(aws.ToString(detail.Arn)); arn != "" {
				arns = append(arns, arn)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return arns, nil
		}
	}
}

func (c *Client) listAssociatedResources(
	ctx context.Context,
	application string,
) ([]appregistryservice.AssociatedResource, error) {
	application = strings.TrimSpace(application)
	if application == "" {
		return nil, nil
	}
	var resources []appregistryservice.AssociatedResource
	var nextToken *string
	for {
		var page *awsappregistry.ListAssociatedResourcesOutput
		err := c.recordAPICall(ctx, "ListAssociatedResources", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAssociatedResources(callCtx, &awsappregistry.ListAssociatedResourcesInput{
				Application: aws.String(application),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return resources, nil
		}
		for _, info := range page.Resources {
			// Only the resource identity and AppRegistry type are metadata. The
			// ResourceDetails.TagValue and any content detail are intentionally
			// never read so the adapter stays metadata-only.
			resources = append(resources, appregistryservice.AssociatedResource{
				ARN:          strings.TrimSpace(aws.ToString(info.Arn)),
				Name:         strings.TrimSpace(aws.ToString(info.Name)),
				ResourceType: strings.TrimSpace(string(info.ResourceType)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return resources, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsappregistry.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsappregistry.ListTagsForResourceInput{
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
	for key, value := range output.Tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = value
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
		code == "TooManyRequestsException"
}

var _ appregistryservice.Client = (*Client)(nil)

var _ apiClient = (*awsappregistry.Client)(nil)
