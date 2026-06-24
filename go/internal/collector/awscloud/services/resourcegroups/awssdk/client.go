// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK Resource Groups client into the
// scanner-owned metadata-only Client interface. The adapter reads groups, their
// query type, and their membership only; it never calls a mutation API and never
// persists the resource-query body beyond the stack identifier a
// CloudFormation-stack-backed group reports.
package awssdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrg "github.com/aws/aws-sdk-go-v2/service/resourcegroups"
	awsrgtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroups/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	rgservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/resourcegroups"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK Resource Groups surface the adapter needs. It
// is intentionally read-only: it exposes only ListGroups, GetGroupQuery, and
// ListGroupResources, so the adapter cannot reach CreateGroup, UpdateGroup,
// DeleteGroup, UpdateGroupQuery, GroupResources, UngroupResources, Tag, Untag,
// or PutGroupConfiguration.
type apiClient interface {
	ListGroups(context.Context, *awsrg.ListGroupsInput, ...func(*awsrg.Options)) (*awsrg.ListGroupsOutput, error)
	GetGroupQuery(context.Context, *awsrg.GetGroupQueryInput, ...func(*awsrg.Options)) (*awsrg.GetGroupQueryOutput, error)
	ListGroupResources(context.Context, *awsrg.ListGroupResourcesInput, ...func(*awsrg.Options)) (*awsrg.ListGroupResourcesOutput, error)
}

// Client adapts AWS SDK Resource Groups pagination into scanner-owned metadata.
// The adapter never calls a mutation API and never persists the resource-query
// body; it records the query type and, for CloudFormation-stack-backed groups,
// the stack identifier the query reports.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Resource Groups SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsrg.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListGroups returns every Resource Groups group in the claimed boundary, each
// enriched with its query type and member resources. Group descriptions come
// from the ListGroups identifiers; the query type and stack identifier come from
// GetGroupQuery; the members come from ListGroupResources.
func (c *Client) ListGroups(ctx context.Context) ([]rgservice.Group, error) {
	identifiers, err := c.listGroupIdentifiers(ctx)
	if err != nil {
		return nil, err
	}
	groups := make([]rgservice.Group, 0, len(identifiers))
	for _, identifier := range identifiers {
		group, err := c.groupMetadata(ctx, identifier)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func (c *Client) listGroupIdentifiers(ctx context.Context) ([]awsrgtypes.GroupIdentifier, error) {
	paginator := awsrg.NewListGroupsPaginator(c.client, &awsrg.ListGroupsInput{})
	var identifiers []awsrgtypes.GroupIdentifier
	for paginator.HasMorePages() {
		var page *awsrg.ListGroupsOutput
		err := c.recordAPICall(ctx, "ListGroups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		identifiers = append(identifiers, page.GroupIdentifiers...)
	}
	return identifiers, nil
}

func (c *Client) groupMetadata(
	ctx context.Context,
	identifier awsrgtypes.GroupIdentifier,
) (rgservice.Group, error) {
	groupARN := strings.TrimSpace(aws.ToString(identifier.GroupArn))
	name := strings.TrimSpace(aws.ToString(identifier.GroupName))
	queryType, stackIdentifier, err := c.groupQuery(ctx, groupARN, name)
	if err != nil {
		return rgservice.Group{}, err
	}
	members, err := c.groupResources(ctx, groupARN, name)
	if err != nil {
		return rgservice.Group{}, err
	}
	return rgservice.Group{
		ARN:             groupARN,
		Name:            name,
		Description:     strings.TrimSpace(aws.ToString(identifier.Description)),
		QueryType:       queryType,
		StackIdentifier: stackIdentifier,
		Members:         members,
	}, nil
}

// groupQuery reads the group's resource-query type and, for a
// CloudFormation-stack-backed group, the stack identifier the query reports. The
// resource-query body itself is never returned beyond the stack identifier; only
// the query type and that identity are kept.
func (c *Client) groupQuery(ctx context.Context, groupARN, name string) (queryType, stackIdentifier string, err error) {
	var out *awsrg.GetGroupQueryOutput
	callErr := c.recordAPICall(ctx, "GetGroupQuery", func(callCtx context.Context) error {
		var inner error
		out, inner = c.client.GetGroupQuery(callCtx, &awsrg.GetGroupQueryInput{
			Group: aws.String(groupRef(groupARN, name)),
		})
		return inner
	})
	if callErr != nil {
		return "", "", callErr
	}
	if out == nil || out.GroupQuery == nil || out.GroupQuery.ResourceQuery == nil {
		return "", "", nil
	}
	query := out.GroupQuery.ResourceQuery
	queryType = string(query.Type)
	if queryType == queryTypeCloudFormationStack {
		stackIdentifier = stackIdentifierFromQuery(aws.ToString(query.Query))
	}
	return queryType, stackIdentifier, nil
}

func (c *Client) groupResources(ctx context.Context, groupARN, name string) ([]rgservice.ResourceMember, error) {
	paginator := awsrg.NewListGroupResourcesPaginator(c.client, &awsrg.ListGroupResourcesInput{
		Group: aws.String(groupRef(groupARN, name)),
	})
	var members []rgservice.ResourceMember
	for paginator.HasMorePages() {
		var page *awsrg.ListGroupResourcesOutput
		err := c.recordAPICall(ctx, "ListGroupResources", func(callCtx context.Context) error {
			var inner error
			page, inner = paginator.NextPage(callCtx)
			return inner
		})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Resources {
			if item.Identifier == nil {
				continue
			}
			arn := strings.TrimSpace(aws.ToString(item.Identifier.ResourceArn))
			if arn == "" {
				continue
			}
			members = append(members, rgservice.ResourceMember{
				ARN:          arn,
				ResourceType: strings.TrimSpace(aws.ToString(item.Identifier.ResourceType)),
			})
		}
	}
	return members, nil
}

// queryTypeCloudFormationStack mirrors the scanner-side constant so the adapter
// only extracts a stack identifier for a CloudFormation-stack-backed group.
const queryTypeCloudFormationStack = "CLOUDFORMATION_STACK_1_0"

// stackIdentifierFromQuery extracts the StackIdentifier the
// CLOUDFORMATION_STACK resource-query body reports. The query body is a small
// JSON object; only the stack identity is read, never any other field. It
// returns "" when the body is absent or unparseable.
func stackIdentifierFromQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	var decoded struct {
		StackIdentifier string `json:"StackIdentifier"`
	}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return ""
	}
	return strings.TrimSpace(decoded.StackIdentifier)
}

// groupRef returns the identifier to pass to the Group request parameter,
// preferring the ARN and falling back to the name.
func groupRef(groupARN, name string) string {
	if trimmed := strings.TrimSpace(groupARN); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(name)
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

var (
	_ rgservice.Client = (*Client)(nil)
	_ apiClient        = (*awsrg.Client)(nil)
)
