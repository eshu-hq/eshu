// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnm "github.com/aws/aws-sdk-go-v2/service/networkmanager"
	awsnmtypes "github.com/aws/aws-sdk-go-v2/service/networkmanager/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	nmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkmanager"
)

// globalServiceRegion returns the single AWS Region that hosts the Network
// Manager control plane for a partition. Network Manager is global, but every
// API call must target the partition's control-plane region; commercial uses
// us-west-2, GovCloud us-gov-west-1, and China cn-north-1.
func globalServiceRegion(partition string) string {
	switch partition {
	case awscloud.PartitionGovCloud:
		return "us-gov-west-1"
	case awscloud.PartitionChina:
		return "cn-north-1"
	default:
		return "us-west-2"
	}
}

func (c *Client) describeGlobalNetworks(ctx context.Context) ([]nmservice.GlobalNetwork, error) {
	var networks []nmservice.GlobalNetwork
	var token *string
	for {
		var page *awsnm.DescribeGlobalNetworksOutput
		err := c.recordAPICall(ctx, "DescribeGlobalNetworks", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeGlobalNetworks(callCtx, &awsnm.DescribeGlobalNetworksInput{
				NextToken: token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return networks, nil
		}
		for _, network := range page.GlobalNetworks {
			networks = append(networks, mapGlobalNetwork(network))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return networks, nil
		}
	}
}

func (c *Client) getSites(ctx context.Context, globalNetworkID string) ([]nmservice.Site, error) {
	var sites []nmservice.Site
	var token *string
	for {
		var page *awsnm.GetSitesOutput
		err := c.recordAPICall(ctx, "GetSites", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetSites(callCtx, &awsnm.GetSitesInput{
				GlobalNetworkId: aws.String(globalNetworkID),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return sites, nil
		}
		for _, site := range page.Sites {
			sites = append(sites, mapSite(site))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return sites, nil
		}
	}
}

func (c *Client) getDevices(ctx context.Context, globalNetworkID string) ([]nmservice.Device, error) {
	var devices []nmservice.Device
	var token *string
	for {
		var page *awsnm.GetDevicesOutput
		err := c.recordAPICall(ctx, "GetDevices", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetDevices(callCtx, &awsnm.GetDevicesInput{
				GlobalNetworkId: aws.String(globalNetworkID),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return devices, nil
		}
		for _, device := range page.Devices {
			devices = append(devices, mapDevice(device))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return devices, nil
		}
	}
}

func (c *Client) getLinks(ctx context.Context, globalNetworkID string) ([]nmservice.Link, error) {
	var links []nmservice.Link
	var token *string
	for {
		var page *awsnm.GetLinksOutput
		err := c.recordAPICall(ctx, "GetLinks", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetLinks(callCtx, &awsnm.GetLinksInput{
				GlobalNetworkId: aws.String(globalNetworkID),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return links, nil
		}
		for _, link := range page.Links {
			links = append(links, mapLink(link))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return links, nil
		}
	}
}

func (c *Client) getConnections(ctx context.Context, globalNetworkID string) ([]nmservice.Connection, error) {
	var connections []nmservice.Connection
	var token *string
	for {
		var page *awsnm.GetConnectionsOutput
		err := c.recordAPICall(ctx, "GetConnections", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetConnections(callCtx, &awsnm.GetConnectionsInput{
				GlobalNetworkId: aws.String(globalNetworkID),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return connections, nil
		}
		for _, connection := range page.Connections {
			connections = append(connections, mapConnection(connection))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return connections, nil
		}
	}
}

func (c *Client) getLinkAssociations(ctx context.Context, globalNetworkID string) ([]nmservice.LinkAssociation, error) {
	var associations []nmservice.LinkAssociation
	var token *string
	for {
		var page *awsnm.GetLinkAssociationsOutput
		err := c.recordAPICall(ctx, "GetLinkAssociations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetLinkAssociations(callCtx, &awsnm.GetLinkAssociationsInput{
				GlobalNetworkId: aws.String(globalNetworkID),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return associations, nil
		}
		for _, association := range page.LinkAssociations {
			associations = append(associations, mapLinkAssociation(association))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return associations, nil
		}
	}
}

func (c *Client) getTransitGatewayRegistrations(
	ctx context.Context,
	globalNetworkID string,
) ([]nmservice.TransitGatewayRegistration, error) {
	var registrations []nmservice.TransitGatewayRegistration
	var token *string
	for {
		var page *awsnm.GetTransitGatewayRegistrationsOutput
		err := c.recordAPICall(ctx, "GetTransitGatewayRegistrations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetTransitGatewayRegistrations(callCtx, &awsnm.GetTransitGatewayRegistrationsInput{
				GlobalNetworkId: aws.String(globalNetworkID),
				NextToken:       token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return registrations, nil
		}
		for _, registration := range page.TransitGatewayRegistrations {
			registrations = append(registrations, mapTransitGatewayRegistration(registration))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return registrations, nil
		}
	}
}

// listCoreNetworks lists every core network summary and resolves each one to its
// full description so segment and edge metadata is available.
func (c *Client) listCoreNetworks(ctx context.Context) ([]nmservice.CoreNetwork, error) {
	var summaries []awsnmtypes.CoreNetworkSummary
	var token *string
	for {
		var page *awsnm.ListCoreNetworksOutput
		err := c.recordAPICall(ctx, "ListCoreNetworks", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListCoreNetworks(callCtx, &awsnm.ListCoreNetworksInput{
				NextToken: token,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		summaries = append(summaries, page.CoreNetworks...)
		token = page.NextToken
		if aws.ToString(token) == "" {
			break
		}
	}

	coreNetworks := make([]nmservice.CoreNetwork, 0, len(summaries))
	for _, summary := range summaries {
		core, err := c.getCoreNetwork(ctx, summary)
		if err != nil {
			return nil, err
		}
		coreNetworks = append(coreNetworks, core)
	}
	return coreNetworks, nil
}

func (c *Client) getCoreNetwork(
	ctx context.Context,
	summary awsnmtypes.CoreNetworkSummary,
) (nmservice.CoreNetwork, error) {
	id := strings.TrimSpace(aws.ToString(summary.CoreNetworkId))
	if id == "" {
		return mapCoreNetworkSummary(summary), nil
	}
	var output *awsnm.GetCoreNetworkOutput
	err := c.recordAPICall(ctx, "GetCoreNetwork", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetCoreNetwork(callCtx, &awsnm.GetCoreNetworkInput{
			CoreNetworkId: aws.String(id),
		})
		return callErr
	})
	if err != nil {
		return nmservice.CoreNetwork{}, err
	}
	if output == nil || output.CoreNetwork == nil {
		return mapCoreNetworkSummary(summary), nil
	}
	return mapCoreNetwork(*output.CoreNetwork), nil
}
