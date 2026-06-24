// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsidentity "github.com/aws/aws-sdk-go-v2/service/cognitoidentity"
	awsidentitytypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentity/types"
	awsidp "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cognitoservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cognito"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listUserPoolsPageSize bounds each ListUserPools page. The API requires a
// MaxResults value (no default), and 60 is the service maximum.
const listUserPoolsPageSize = 60

// userPoolClientAPI is the cognito-idp read surface the adapter uses.
//
// It deliberately omits ListUsers, AdminGetUser, AdminListGroupsForUser,
// ListUsersInGroup, ListUserPoolClientSecrets, and every Create/Update/Delete
// operation. DescribeUserPoolClient and DescribeIdentityProvider are listed
// because the OAuth-flow and provider-type metadata require them, but the
// adapter never maps their secret-bearing fields (ClientSecret, ProviderDetails)
// into scanner types.
type userPoolClientAPI interface {
	ListUserPools(context.Context, *awsidp.ListUserPoolsInput, ...func(*awsidp.Options)) (*awsidp.ListUserPoolsOutput, error)
	DescribeUserPool(context.Context, *awsidp.DescribeUserPoolInput, ...func(*awsidp.Options)) (*awsidp.DescribeUserPoolOutput, error)
	ListUserPoolClients(context.Context, *awsidp.ListUserPoolClientsInput, ...func(*awsidp.Options)) (*awsidp.ListUserPoolClientsOutput, error)
	DescribeUserPoolClient(context.Context, *awsidp.DescribeUserPoolClientInput, ...func(*awsidp.Options)) (*awsidp.DescribeUserPoolClientOutput, error)
	ListIdentityProviders(context.Context, *awsidp.ListIdentityProvidersInput, ...func(*awsidp.Options)) (*awsidp.ListIdentityProvidersOutput, error)
	ListResourceServers(context.Context, *awsidp.ListResourceServersInput, ...func(*awsidp.Options)) (*awsidp.ListResourceServersOutput, error)
	ListGroups(context.Context, *awsidp.ListGroupsInput, ...func(*awsidp.Options)) (*awsidp.ListGroupsOutput, error)
}

// identityPoolAPI is the cognito-identity read surface the adapter uses. It
// omits ListIdentities, DescribeIdentity, GetId, GetCredentialsForIdentity,
// GetOpenIdToken*, and every mutation. Those reach identity records (PII) or
// mint credentials.
type identityPoolAPI interface {
	ListIdentityPools(context.Context, *awsidentity.ListIdentityPoolsInput, ...func(*awsidentity.Options)) (*awsidentity.ListIdentityPoolsOutput, error)
	DescribeIdentityPool(context.Context, *awsidentity.DescribeIdentityPoolInput, ...func(*awsidentity.Options)) (*awsidentity.DescribeIdentityPoolOutput, error)
	GetIdentityPoolRoles(context.Context, *awsidentity.GetIdentityPoolRolesInput, ...func(*awsidentity.Options)) (*awsidentity.GetIdentityPoolRolesOutput, error)
}

// Client adapts AWS SDK Cognito calls into scanner-owned Cognito metadata.
type Client struct {
	userPoolClient userPoolClientAPI
	identityClient identityPoolAPI
	boundary       awscloud.Boundary
	tracer         trace.Tracer
	instruments    *telemetry.Instruments
}

// NewClient builds a Cognito SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		userPoolClient: awsidp.NewFromConfig(config),
		identityClient: awsidentity.NewFromConfig(config),
		boundary:       boundary,
		tracer:         tracer,
		instruments:    instruments,
	}
}

// ListUserPools returns user pools with their describe-level metadata.
func (c *Client) ListUserPools(ctx context.Context) ([]cognitoservice.UserPool, error) {
	var pools []cognitoservice.UserPool
	var nextToken *string
	for {
		var page *awsidp.ListUserPoolsOutput
		err := c.recordAPICall(ctx, "ListUserPools", func(callCtx context.Context) error {
			var err error
			page, err = c.userPoolClient.ListUserPools(callCtx, &awsidp.ListUserPoolsInput{
				MaxResults: aws.Int32(listUserPoolsPageSize),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.UserPools {
			pool, err := c.describeUserPool(ctx, aws.ToString(summary.Id))
			if err != nil {
				return nil, err
			}
			if pool != nil {
				pools = append(pools, *pool)
			}
		}
		if page.NextToken == nil || strings.TrimSpace(aws.ToString(page.NextToken)) == "" {
			break
		}
		nextToken = page.NextToken
	}
	return pools, nil
}

func (c *Client) describeUserPool(ctx context.Context, poolID string) (*cognitoservice.UserPool, error) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, nil
	}
	var output *awsidp.DescribeUserPoolOutput
	err := c.recordAPICall(ctx, "DescribeUserPool", func(callCtx context.Context) error {
		var err error
		output, err = c.userPoolClient.DescribeUserPool(callCtx, &awsidp.DescribeUserPoolInput{
			UserPoolId: aws.String(poolID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.UserPool == nil {
		return nil, nil
	}
	pool := mapUserPool(*output.UserPool)
	return &pool, nil
}

// ListUserPoolClients returns app-client metadata for one user pool. It never
// returns or persists ClientSecret.
func (c *Client) ListUserPoolClients(ctx context.Context, poolID string) ([]cognitoservice.UserPoolClient, error) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, nil
	}
	var clientIDs []string
	var nextToken *string
	for {
		var page *awsidp.ListUserPoolClientsOutput
		err := c.recordAPICall(ctx, "ListUserPoolClients", func(callCtx context.Context) error {
			var err error
			page, err = c.userPoolClient.ListUserPoolClients(callCtx, &awsidp.ListUserPoolClientsInput{
				UserPoolId: aws.String(poolID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, summary := range page.UserPoolClients {
			clientIDs = append(clientIDs, aws.ToString(summary.ClientId))
		}
		if page.NextToken == nil || strings.TrimSpace(aws.ToString(page.NextToken)) == "" {
			break
		}
		nextToken = page.NextToken
	}
	var clients []cognitoservice.UserPoolClient
	for _, clientID := range clientIDs {
		client, err := c.describeUserPoolClient(ctx, poolID, clientID)
		if err != nil {
			return nil, err
		}
		if client != nil {
			clients = append(clients, *client)
		}
	}
	return clients, nil
}

func (c *Client) describeUserPoolClient(ctx context.Context, poolID, clientID string) (*cognitoservice.UserPoolClient, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, nil
	}
	var output *awsidp.DescribeUserPoolClientOutput
	err := c.recordAPICall(ctx, "DescribeUserPoolClient", func(callCtx context.Context) error {
		var err error
		output, err = c.userPoolClient.DescribeUserPoolClient(callCtx, &awsidp.DescribeUserPoolClientInput{
			UserPoolId: aws.String(poolID),
			ClientId:   aws.String(clientID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.UserPoolClient == nil {
		return nil, nil
	}
	client := mapUserPoolClient(*output.UserPoolClient)
	return &client, nil
}

// ListIdentityProviders returns provider metadata for one user pool. It never
// returns or persists ProviderDetails.
func (c *Client) ListIdentityProviders(ctx context.Context, poolID string) ([]cognitoservice.IdentityProvider, error) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, nil
	}
	var providers []cognitoservice.IdentityProvider
	var nextToken *string
	for {
		var page *awsidp.ListIdentityProvidersOutput
		err := c.recordAPICall(ctx, "ListIdentityProviders", func(callCtx context.Context) error {
			var err error
			page, err = c.userPoolClient.ListIdentityProviders(callCtx, &awsidp.ListIdentityProvidersInput{
				UserPoolId: aws.String(poolID),
				MaxResults: aws.Int32(60),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, provider := range page.Providers {
			providers = append(providers, mapIdentityProvider(poolID, provider))
		}
		if page.NextToken == nil || strings.TrimSpace(aws.ToString(page.NextToken)) == "" {
			break
		}
		nextToken = page.NextToken
	}
	return providers, nil
}

// ListResourceServers returns resource-server metadata for one user pool.
func (c *Client) ListResourceServers(ctx context.Context, poolID string) ([]cognitoservice.ResourceServer, error) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, nil
	}
	var resourceServers []cognitoservice.ResourceServer
	var nextToken *string
	for {
		var page *awsidp.ListResourceServersOutput
		err := c.recordAPICall(ctx, "ListResourceServers", func(callCtx context.Context) error {
			var err error
			page, err = c.userPoolClient.ListResourceServers(callCtx, &awsidp.ListResourceServersInput{
				UserPoolId: aws.String(poolID),
				MaxResults: aws.Int32(50),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, resourceServer := range page.ResourceServers {
			resourceServers = append(resourceServers, mapResourceServer(resourceServer))
		}
		if page.NextToken == nil || strings.TrimSpace(aws.ToString(page.NextToken)) == "" {
			break
		}
		nextToken = page.NextToken
	}
	return resourceServers, nil
}

// ListGroups returns group metadata for one user pool. It reads no user
// membership.
func (c *Client) ListGroups(ctx context.Context, poolID string) ([]cognitoservice.Group, error) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, nil
	}
	var groups []cognitoservice.Group
	var nextToken *string
	for {
		var page *awsidp.ListGroupsOutput
		err := c.recordAPICall(ctx, "ListGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.userPoolClient.ListGroups(callCtx, &awsidp.ListGroupsInput{
				UserPoolId: aws.String(poolID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, group := range page.Groups {
			groups = append(groups, mapGroup(group))
		}
		if page.NextToken == nil || strings.TrimSpace(aws.ToString(page.NextToken)) == "" {
			break
		}
		nextToken = page.NextToken
	}
	return groups, nil
}

// ListIdentityPools returns identity pools with describe-level metadata and a
// role-attachment summary.
func (c *Client) ListIdentityPools(ctx context.Context) ([]cognitoservice.IdentityPool, error) {
	var summaries []awsidentitytypes.IdentityPoolShortDescription
	var nextToken *string
	for {
		var page *awsidentity.ListIdentityPoolsOutput
		err := c.recordAPICall(ctx, "ListIdentityPools", func(callCtx context.Context) error {
			var err error
			page, err = c.identityClient.ListIdentityPools(callCtx, &awsidentity.ListIdentityPoolsInput{
				MaxResults: aws.Int32(60),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, page.IdentityPools...)
		if page.NextToken == nil || strings.TrimSpace(aws.ToString(page.NextToken)) == "" {
			break
		}
		nextToken = page.NextToken
	}
	var pools []cognitoservice.IdentityPool
	for _, summary := range summaries {
		pool, err := c.describeIdentityPool(ctx, aws.ToString(summary.IdentityPoolId))
		if err != nil {
			return nil, err
		}
		if pool != nil {
			pools = append(pools, *pool)
		}
	}
	return pools, nil
}

func (c *Client) describeIdentityPool(ctx context.Context, poolID string) (*cognitoservice.IdentityPool, error) {
	poolID = strings.TrimSpace(poolID)
	if poolID == "" {
		return nil, nil
	}
	var output *awsidentity.DescribeIdentityPoolOutput
	err := c.recordAPICall(ctx, "DescribeIdentityPool", func(callCtx context.Context) error {
		var err error
		output, err = c.identityClient.DescribeIdentityPool(callCtx, &awsidentity.DescribeIdentityPoolInput{
			IdentityPoolId: aws.String(poolID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	roles, err := c.identityPoolRoles(ctx, poolID)
	if err != nil {
		return nil, err
	}
	pool := mapIdentityPool(output, c.boundary, roles)
	return &pool, nil
}

func (c *Client) identityPoolRoles(ctx context.Context, poolID string) (map[string]string, error) {
	var output *awsidentity.GetIdentityPoolRolesOutput
	err := c.recordAPICall(ctx, "GetIdentityPoolRoles", func(callCtx context.Context) error {
		var err error
		output, err = c.identityClient.GetIdentityPoolRoles(callCtx, &awsidentity.GetIdentityPoolRolesInput{
			IdentityPoolId: aws.String(poolID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Roles, nil
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

var _ cognitoservice.Client = (*Client)(nil)

var _ userPoolClientAPI = (*awsidp.Client)(nil)

var _ identityPoolAPI = (*awsidentity.Client)(nil)
