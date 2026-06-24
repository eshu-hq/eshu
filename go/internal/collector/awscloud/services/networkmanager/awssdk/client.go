// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnm "github.com/aws/aws-sdk-go-v2/service/networkmanager"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	nmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkmanager"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Network Manager API the
// adapter calls. It lists global networks and core networks and gets the sites,
// devices, links, connections, link associations, and transit gateway
// registrations within each global network. It exposes no Create/Update/Delete,
// no Register/Deregister, no Associate/Disassociate, no Start/Tag/Put mutation,
// and no route-analysis start, so the adapter cannot mutate Network Manager
// state. The exclusion_test reflects over this interface to enforce that
// contract at build time.
type apiClient interface {
	DescribeGlobalNetworks(
		context.Context,
		*awsnm.DescribeGlobalNetworksInput,
		...func(*awsnm.Options),
	) (*awsnm.DescribeGlobalNetworksOutput, error)
	GetSites(
		context.Context,
		*awsnm.GetSitesInput,
		...func(*awsnm.Options),
	) (*awsnm.GetSitesOutput, error)
	GetDevices(
		context.Context,
		*awsnm.GetDevicesInput,
		...func(*awsnm.Options),
	) (*awsnm.GetDevicesOutput, error)
	GetLinks(
		context.Context,
		*awsnm.GetLinksInput,
		...func(*awsnm.Options),
	) (*awsnm.GetLinksOutput, error)
	GetConnections(
		context.Context,
		*awsnm.GetConnectionsInput,
		...func(*awsnm.Options),
	) (*awsnm.GetConnectionsOutput, error)
	GetLinkAssociations(
		context.Context,
		*awsnm.GetLinkAssociationsInput,
		...func(*awsnm.Options),
	) (*awsnm.GetLinkAssociationsOutput, error)
	GetTransitGatewayRegistrations(
		context.Context,
		*awsnm.GetTransitGatewayRegistrationsInput,
		...func(*awsnm.Options),
	) (*awsnm.GetTransitGatewayRegistrationsOutput, error)
	ListCoreNetworks(
		context.Context,
		*awsnm.ListCoreNetworksInput,
		...func(*awsnm.Options),
	) (*awsnm.ListCoreNetworksOutput, error)
	GetCoreNetwork(
		context.Context,
		*awsnm.GetCoreNetworkInput,
		...func(*awsnm.Options),
	) (*awsnm.GetCoreNetworkOutput, error)
}

// Client adapts AWS SDK Network Manager control-plane reads into scanner-owned
// metadata. It never mutates a Network Manager resource and exposes only
// Describe/Get/List operations.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Network Manager SDK adapter for one claimed AWS boundary.
// Network Manager is a global service whose control plane is reachable only in
// one region per partition, so the client region is pinned to that region
// (derived from the boundary partition) regardless of the claim region. This
// keeps GovCloud and China claims hitting the correct partition endpoint instead
// of failing against the commercial region.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	region := globalServiceRegion(awscloud.PartitionForBoundary(boundary))
	return &Client{
		client: awsnm.NewFromConfig(config, func(o *awsnm.Options) {
			o.Region = region
		}),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Network Manager global-network metadata, the child resources
// under each global network, and the account's core networks. It never mutates
// Network Manager state.
func (c *Client) Snapshot(ctx context.Context) (nmservice.Snapshot, error) {
	networks, err := c.describeGlobalNetworks(ctx)
	if err != nil {
		return nmservice.Snapshot{}, err
	}
	for i := range networks {
		if err := c.fillGlobalNetwork(ctx, &networks[i]); err != nil {
			return nmservice.Snapshot{}, err
		}
	}
	coreNetworks, err := c.listCoreNetworks(ctx)
	if err != nil {
		return nmservice.Snapshot{}, err
	}
	return nmservice.Snapshot{GlobalNetworks: networks, CoreNetworks: coreNetworks}, nil
}

// fillGlobalNetwork loads every child resource collection for one global
// network. A blank global network id cannot anchor any child read, so it is
// returned with no children rather than issuing account-wide reads.
func (c *Client) fillGlobalNetwork(ctx context.Context, network *nmservice.GlobalNetwork) error {
	id := strings.TrimSpace(network.ID)
	if id == "" {
		return nil
	}
	var err error
	if network.Sites, err = c.getSites(ctx, id); err != nil {
		return err
	}
	if network.Devices, err = c.getDevices(ctx, id); err != nil {
		return err
	}
	if network.Links, err = c.getLinks(ctx, id); err != nil {
		return err
	}
	if network.Connections, err = c.getConnections(ctx, id); err != nil {
		return err
	}
	if network.LinkAssociations, err = c.getLinkAssociations(ctx, id); err != nil {
		return err
	}
	if network.TransitGatewayRegistrations, err = c.getTransitGatewayRegistrations(ctx, id); err != nil {
		return err
	}
	return nil
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
	_ nmservice.Client = (*Client)(nil)
	_ apiClient        = (*awsnm.Client)(nil)
)
