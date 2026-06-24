// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstransfer "github.com/aws/aws-sdk-go-v2/service/transfer"
	awstransfertypes "github.com/aws/aws-sdk-go-v2/service/transfer/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	transferservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/transfer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the read-only AWS SDK surface the adapter consumes. It exposes
// only ListServers, DescribeServer, ListUsers, and DescribeUser. No create,
// update, delete, start, stop, import-key, or any other mutation or
// key-material API is reachable, which the exclusion guard test enforces by
// reflection.
type apiClient interface {
	ListServers(context.Context, *awstransfer.ListServersInput, ...func(*awstransfer.Options)) (*awstransfer.ListServersOutput, error)
	DescribeServer(context.Context, *awstransfer.DescribeServerInput, ...func(*awstransfer.Options)) (*awstransfer.DescribeServerOutput, error)
	ListUsers(context.Context, *awstransfer.ListUsersInput, ...func(*awstransfer.Options)) (*awstransfer.ListUsersOutput, error)
	DescribeUser(context.Context, *awstransfer.DescribeUserInput, ...func(*awstransfer.Options)) (*awstransfer.DescribeUserOutput, error)
}

// Client adapts AWS SDK Transfer Family pagination into scanner-owned metadata.
// The adapter never calls CreateServer, UpdateServer, DeleteServer,
// StartServer, StopServer, ImportSshPublicKey, ImportHostKey, or any other
// mutation API, and never copies host key fingerprints, SSH public key bodies,
// or user policy JSON into scanner-owned types.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments

	// serverIDsOnce memoizes the claim's ListServers pass so the scanner's
	// ListServers (DescribeServer fan-out) and ListUsers calls share a single
	// paginated ListServers stream instead of each re-listing.
	serverIDsOnce      sync.Once
	cachedServerIDs    []string
	cachedServerIDsErr error
}

// NewClient builds a Transfer Family SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awstransfer.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListServers reads Transfer Family server identities with ListServers and
// follows up with DescribeServer for safe metadata. Host key fingerprints,
// host key material, and login banners are intentionally excluded from the
// scanner-owned mapping even though DescribeServer returns them.
func (c *Client) ListServers(ctx context.Context) ([]transferservice.Server, error) {
	ids, err := c.listServerIDs(ctx)
	if err != nil {
		return nil, err
	}
	servers := make([]transferservice.Server, 0, len(ids))
	for _, id := range ids {
		server, err := c.describeServer(ctx, id)
		if err != nil {
			return nil, err
		}
		if server == nil {
			continue
		}
		servers = append(servers, *server)
	}
	return servers, nil
}

// listServerIDs returns the claim's Transfer server IDs, listing them at most
// once per adapter (the adapter is scoped to one claimed boundary). ListServers
// is paginated and both ListServers (DescribeServer fan-out) and ListUsers
// consume the IDs, so memoizing here keeps the scan to a single ListServers pass.
func (c *Client) listServerIDs(ctx context.Context) ([]string, error) {
	c.serverIDsOnce.Do(func() {
		c.cachedServerIDs, c.cachedServerIDsErr = c.fetchServerIDs(ctx)
	})
	return c.cachedServerIDs, c.cachedServerIDsErr
}

func (c *Client) fetchServerIDs(ctx context.Context) ([]string, error) {
	var ids []string
	var nextToken *string
	for {
		var page *awstransfer.ListServersOutput
		err := c.recordAPICall(ctx, "ListServers", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServers(callCtx, &awstransfer.ListServersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ids, nil
		}
		for _, server := range page.Servers {
			if id := strings.TrimSpace(aws.ToString(server.ServerId)); id != "" {
				ids = append(ids, id)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return ids, nil
		}
	}
}

func (c *Client) describeServer(ctx context.Context, serverID string) (*transferservice.Server, error) {
	trimmed := strings.TrimSpace(serverID)
	if trimmed == "" {
		return nil, nil
	}
	var output *awstransfer.DescribeServerOutput
	err := c.recordAPICall(ctx, "DescribeServer", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeServer(callCtx, &awstransfer.DescribeServerInput{
			ServerId: aws.String(trimmed),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.Server == nil {
		return &transferservice.Server{ServerID: trimmed}, nil
	}
	server := mapServer(*output.Server)
	if server.ServerID == "" {
		server.ServerID = trimmed
	}
	return &server, nil
}

// ListUsers reads Transfer Family service-managed user identities per server
// with ListUsers and follows up with DescribeUser for safe metadata. SSH
// public key bodies, user policy JSON, and POSIX UID/GID material are
// intentionally excluded from the scanner-owned mapping even though
// DescribeUser returns them.
func (c *Client) ListUsers(ctx context.Context) ([]transferservice.User, error) {
	serverIDs, err := c.listServerIDs(ctx)
	if err != nil {
		return nil, err
	}
	var users []transferservice.User
	for _, serverID := range serverIDs {
		names, err := c.listUserNames(ctx, serverID)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			user, err := c.describeUser(ctx, serverID, name)
			if err != nil {
				return nil, err
			}
			if user == nil {
				continue
			}
			users = append(users, *user)
		}
	}
	return users, nil
}

func (c *Client) listUserNames(ctx context.Context, serverID string) ([]string, error) {
	trimmed := strings.TrimSpace(serverID)
	if trimmed == "" {
		return nil, nil
	}
	var names []string
	var nextToken *string
	for {
		var page *awstransfer.ListUsersOutput
		err := c.recordAPICall(ctx, "ListUsers", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListUsers(callCtx, &awstransfer.ListUsersInput{
				ServerId:  aws.String(trimmed),
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return names, nil
		}
		for _, user := range page.Users {
			if name := strings.TrimSpace(aws.ToString(user.UserName)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return names, nil
		}
	}
}

func (c *Client) describeUser(ctx context.Context, serverID, userName string) (*transferservice.User, error) {
	trimmedServer := strings.TrimSpace(serverID)
	trimmedUser := strings.TrimSpace(userName)
	if trimmedServer == "" || trimmedUser == "" {
		return nil, nil
	}
	var output *awstransfer.DescribeUserOutput
	err := c.recordAPICall(ctx, "DescribeUser", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeUser(callCtx, &awstransfer.DescribeUserInput{
			ServerId: aws.String(trimmedServer),
			UserName: aws.String(trimmedUser),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.User == nil {
		return &transferservice.User{ServerID: trimmedServer, UserName: trimmedUser}, nil
	}
	user := mapUser(trimmedServer, *output.User)
	if user.ServerID == "" {
		user.ServerID = trimmedServer
	}
	if user.UserName == "" {
		user.UserName = trimmedUser
	}
	return &user, nil
}

func mapServer(server awstransfertypes.DescribedServer) transferservice.Server {
	mapped := transferservice.Server{
		ARN:                       strings.TrimSpace(aws.ToString(server.Arn)),
		ServerID:                  strings.TrimSpace(aws.ToString(server.ServerId)),
		Domain:                    strings.TrimSpace(string(server.Domain)),
		EndpointType:              strings.TrimSpace(string(server.EndpointType)),
		IdentityProviderType:      strings.TrimSpace(string(server.IdentityProviderType)),
		State:                     strings.TrimSpace(string(server.State)),
		UserCount:                 aws.ToInt32(server.UserCount),
		SecurityPolicyName:        strings.TrimSpace(aws.ToString(server.SecurityPolicyName)),
		IPAddressType:             strings.TrimSpace(string(server.IpAddressType)),
		CertificateARN:            strings.TrimSpace(aws.ToString(server.Certificate)),
		LoggingRoleARN:            strings.TrimSpace(aws.ToString(server.LoggingRole)),
		StructuredLogDestinations: cloneStrings(server.StructuredLogDestinations),
	}
	for _, protocol := range server.Protocols {
		if value := strings.TrimSpace(string(protocol)); value != "" {
			mapped.Protocols = append(mapped.Protocols, value)
		}
	}
	if server.EndpointDetails != nil {
		mapped.VPCEndpointID = strings.TrimSpace(aws.ToString(server.EndpointDetails.VpcEndpointId))
		mapped.VPCID = strings.TrimSpace(aws.ToString(server.EndpointDetails.VpcId))
		mapped.AddressAllocationIDs = cloneStrings(server.EndpointDetails.AddressAllocationIds)
		mapped.SubnetIDs = cloneStrings(server.EndpointDetails.SubnetIds)
		mapped.SecurityGroupIDs = cloneStrings(server.EndpointDetails.SecurityGroupIds)
	}
	// HostKeyFingerprint, PreAuthenticationLoginBanner,
	// PostAuthenticationLoginBanner, and IdentityProviderDetails invocation
	// secrets are deliberately NOT mapped: host key material and login banners
	// stay inside AWS.
	return mapped
}

func mapUser(serverID string, user awstransfertypes.DescribedUser) transferservice.User {
	mapped := transferservice.User{
		ServerID:          strings.TrimSpace(serverID),
		ARN:               strings.TrimSpace(aws.ToString(user.Arn)),
		UserName:          strings.TrimSpace(aws.ToString(user.UserName)),
		HomeDirectory:     strings.TrimSpace(aws.ToString(user.HomeDirectory)),
		HomeDirectoryType: strings.TrimSpace(string(user.HomeDirectoryType)),
		RoleARN:           strings.TrimSpace(aws.ToString(user.Role)),
	}
	for _, mapping := range user.HomeDirectoryMappings {
		entry := strings.TrimSpace(aws.ToString(mapping.Entry))
		target := strings.TrimSpace(aws.ToString(mapping.Target))
		if entry == "" && target == "" {
			continue
		}
		mapped.HomeDirectoryMappings = append(mapped.HomeDirectoryMappings, transferservice.HomeDirectoryMapping{
			Entry:  entry,
			Target: target,
		})
	}
	// SshPublicKeys (key bodies), Policy (scope-down policy JSON), and
	// PosixProfile (UID/GID credential material) are deliberately NOT mapped:
	// key material and policy bodies stay inside AWS.
	return mapped
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
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

var _ transferservice.Client = (*Client)(nil)

var _ apiClient = (*awstransfer.Client)(nil)
