// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsram "github.com/aws/aws-sdk-go-v2/service/ram"
	awsramtypes "github.com/aws/aws-sdk-go-v2/service/ram/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ramservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ram"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// resourceOwnerSelf scopes every read to shares this account owns and shares
// out. Shares owned by other accounts (OTHER-ACCOUNTS) are out of scope for the
// owner-account inventory this scanner builds.
const resourceOwnerSelf = awsramtypes.ResourceOwnerSelf

// apiClient is the metadata-only AWS Resource Access Manager read surface used
// by the adapter. Only Get and List reads appear here. No Create/Delete/Update,
// Associate/Disassociate, Accept/Reject, Enable/Disable, Promote/Replace,
// Tag/Untag, or SetDefaultPermissionVersion operation is reachable, and
// GetPermission (which returns the permission policy document body) is
// deliberately absent.
type apiClient interface {
	GetResourceShares(context.Context, *awsram.GetResourceSharesInput, ...func(*awsram.Options)) (*awsram.GetResourceSharesOutput, error)
	ListResources(context.Context, *awsram.ListResourcesInput, ...func(*awsram.Options)) (*awsram.ListResourcesOutput, error)
	ListPrincipals(context.Context, *awsram.ListPrincipalsInput, ...func(*awsram.Options)) (*awsram.ListPrincipalsOutput, error)
	ListResourceSharePermissions(context.Context, *awsram.ListResourceSharePermissionsInput, ...func(*awsram.Options)) (*awsram.ListResourceSharePermissionsOutput, error)
}

// Client adapts the AWS SDK for Go v2 RAM client into scanner-owned resource
// share metadata records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a RAM SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsram.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListResourceShares returns the SELF-owned RAM resource shares visible to the
// configured AWS credentials, each enriched with its shared resources,
// principals, and managed-permission summaries. It never requests a permission
// policy document body.
func (c *Client) ListResourceShares(ctx context.Context) ([]ramservice.ResourceShare, error) {
	shares, err := c.getResourceShares(ctx)
	if err != nil {
		return nil, err
	}
	for i := range shares {
		arn := strings.TrimSpace(shares[i].ARN)
		if arn == "" {
			continue
		}
		resources, err := c.listResources(ctx, arn)
		if err != nil {
			return nil, err
		}
		shares[i].Resources = resources
		principals, err := c.listPrincipals(ctx, arn)
		if err != nil {
			return nil, err
		}
		shares[i].Principals = principals
		permissions, err := c.listSharePermissions(ctx, arn)
		if err != nil {
			return nil, err
		}
		shares[i].Permissions = permissions
	}
	return shares, nil
}

func (c *Client) getResourceShares(ctx context.Context) ([]ramservice.ResourceShare, error) {
	paginator := awsram.NewGetResourceSharesPaginator(c.client, &awsram.GetResourceSharesInput{
		ResourceOwner: resourceOwnerSelf,
	})
	var shares []ramservice.ResourceShare
	for paginator.HasMorePages() {
		var page *awsram.GetResourceSharesOutput
		err := c.recordAPICall(ctx, "GetResourceShares", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, share := range page.ResourceShares {
			shares = append(shares, mapResourceShare(share))
		}
	}
	return shares, nil
}

func (c *Client) listResources(ctx context.Context, shareARN string) ([]ramservice.SharedResource, error) {
	paginator := awsram.NewListResourcesPaginator(c.client, &awsram.ListResourcesInput{
		ResourceOwner:     resourceOwnerSelf,
		ResourceShareArns: []string{shareARN},
	})
	var resources []ramservice.SharedResource
	for paginator.HasMorePages() {
		var page *awsram.ListResourcesOutput
		err := c.recordAPICall(ctx, "ListResources", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, resource := range page.Resources {
			resources = append(resources, mapSharedResource(resource))
		}
	}
	return resources, nil
}

func (c *Client) listPrincipals(ctx context.Context, shareARN string) ([]ramservice.Principal, error) {
	paginator := awsram.NewListPrincipalsPaginator(c.client, &awsram.ListPrincipalsInput{
		ResourceOwner:     resourceOwnerSelf,
		ResourceShareArns: []string{shareARN},
	})
	var principals []ramservice.Principal
	for paginator.HasMorePages() {
		var page *awsram.ListPrincipalsOutput
		err := c.recordAPICall(ctx, "ListPrincipals", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, principal := range page.Principals {
			principals = append(principals, mapPrincipal(principal))
		}
	}
	return principals, nil
}

func (c *Client) listSharePermissions(ctx context.Context, shareARN string) ([]ramservice.Permission, error) {
	paginator := awsram.NewListResourceSharePermissionsPaginator(c.client, &awsram.ListResourceSharePermissionsInput{
		ResourceShareArn: aws.String(shareARN),
	})
	var permissions []ramservice.Permission
	for paginator.HasMorePages() {
		var page *awsram.ListResourceSharePermissionsOutput
		err := c.recordAPICall(ctx, "ListResourceSharePermissions", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, permission := range page.Permissions {
			permissions = append(permissions, mapPermission(permission))
		}
	}
	return permissions, nil
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

var _ ramservice.Client = (*Client)(nil)

var _ apiClient = (*awsram.Client)(nil)
