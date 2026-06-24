// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnmtypes "github.com/aws/aws-sdk-go-v2/service/networkmanager/types"

	nmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkmanager"
)

func mapGlobalNetwork(network awsnmtypes.GlobalNetwork) nmservice.GlobalNetwork {
	return nmservice.GlobalNetwork{
		ARN:         strings.TrimSpace(aws.ToString(network.GlobalNetworkArn)),
		ID:          strings.TrimSpace(aws.ToString(network.GlobalNetworkId)),
		Description: strings.TrimSpace(aws.ToString(network.Description)),
		State:       strings.TrimSpace(string(network.State)),
		CreatedAt:   aws.ToTime(network.CreatedAt),
		Tags:        mapTags(network.Tags),
	}
}

func mapSite(site awsnmtypes.Site) nmservice.Site {
	mapped := nmservice.Site{
		ARN:             strings.TrimSpace(aws.ToString(site.SiteArn)),
		ID:              strings.TrimSpace(aws.ToString(site.SiteId)),
		GlobalNetworkID: strings.TrimSpace(aws.ToString(site.GlobalNetworkId)),
		Description:     strings.TrimSpace(aws.ToString(site.Description)),
		State:           strings.TrimSpace(string(site.State)),
		CreatedAt:       aws.ToTime(site.CreatedAt),
		Tags:            mapTags(site.Tags),
	}
	if loc := site.Location; loc != nil {
		mapped.Address = strings.TrimSpace(aws.ToString(loc.Address))
		mapped.Latitude = strings.TrimSpace(aws.ToString(loc.Latitude))
		mapped.Longitude = strings.TrimSpace(aws.ToString(loc.Longitude))
	}
	return mapped
}

func mapDevice(device awsnmtypes.Device) nmservice.Device {
	mapped := nmservice.Device{
		ARN:             strings.TrimSpace(aws.ToString(device.DeviceArn)),
		ID:              strings.TrimSpace(aws.ToString(device.DeviceId)),
		GlobalNetworkID: strings.TrimSpace(aws.ToString(device.GlobalNetworkId)),
		SiteID:          strings.TrimSpace(aws.ToString(device.SiteId)),
		Description:     strings.TrimSpace(aws.ToString(device.Description)),
		Type:            strings.TrimSpace(aws.ToString(device.Type)),
		Vendor:          strings.TrimSpace(aws.ToString(device.Vendor)),
		Model:           strings.TrimSpace(aws.ToString(device.Model)),
		State:           strings.TrimSpace(string(device.State)),
		CreatedAt:       aws.ToTime(device.CreatedAt),
		Tags:            mapTags(device.Tags),
	}
	if loc := device.AWSLocation; loc != nil {
		mapped.SubnetARN = strings.TrimSpace(aws.ToString(loc.SubnetArn))
		mapped.Zone = strings.TrimSpace(aws.ToString(loc.Zone))
	}
	if loc := device.Location; loc != nil {
		mapped.Address = strings.TrimSpace(aws.ToString(loc.Address))
		mapped.Latitude = strings.TrimSpace(aws.ToString(loc.Latitude))
		mapped.Longitude = strings.TrimSpace(aws.ToString(loc.Longitude))
	}
	return mapped
}

func mapLink(link awsnmtypes.Link) nmservice.Link {
	mapped := nmservice.Link{
		ARN:             strings.TrimSpace(aws.ToString(link.LinkArn)),
		ID:              strings.TrimSpace(aws.ToString(link.LinkId)),
		GlobalNetworkID: strings.TrimSpace(aws.ToString(link.GlobalNetworkId)),
		SiteID:          strings.TrimSpace(aws.ToString(link.SiteId)),
		Description:     strings.TrimSpace(aws.ToString(link.Description)),
		Type:            strings.TrimSpace(aws.ToString(link.Type)),
		Provider:        strings.TrimSpace(aws.ToString(link.Provider)),
		State:           strings.TrimSpace(string(link.State)),
		CreatedAt:       aws.ToTime(link.CreatedAt),
		Tags:            mapTags(link.Tags),
	}
	if bandwidth := link.Bandwidth; bandwidth != nil {
		mapped.UploadSpeedMbps = aws.ToInt32(bandwidth.UploadSpeed)
		mapped.DownloadSpeedMbps = aws.ToInt32(bandwidth.DownloadSpeed)
	}
	return mapped
}

func mapConnection(connection awsnmtypes.Connection) nmservice.Connection {
	return nmservice.Connection{
		ARN:               strings.TrimSpace(aws.ToString(connection.ConnectionArn)),
		ID:                strings.TrimSpace(aws.ToString(connection.ConnectionId)),
		GlobalNetworkID:   strings.TrimSpace(aws.ToString(connection.GlobalNetworkId)),
		DeviceID:          strings.TrimSpace(aws.ToString(connection.DeviceId)),
		ConnectedDeviceID: strings.TrimSpace(aws.ToString(connection.ConnectedDeviceId)),
		LinkID:            strings.TrimSpace(aws.ToString(connection.LinkId)),
		ConnectedLinkID:   strings.TrimSpace(aws.ToString(connection.ConnectedLinkId)),
		Description:       strings.TrimSpace(aws.ToString(connection.Description)),
		State:             strings.TrimSpace(string(connection.State)),
		CreatedAt:         aws.ToTime(connection.CreatedAt),
		Tags:              mapTags(connection.Tags),
	}
}

func mapLinkAssociation(association awsnmtypes.LinkAssociation) nmservice.LinkAssociation {
	return nmservice.LinkAssociation{
		GlobalNetworkID: strings.TrimSpace(aws.ToString(association.GlobalNetworkId)),
		DeviceID:        strings.TrimSpace(aws.ToString(association.DeviceId)),
		LinkID:          strings.TrimSpace(aws.ToString(association.LinkId)),
		State:           strings.TrimSpace(string(association.LinkAssociationState)),
	}
}

func mapTransitGatewayRegistration(
	registration awsnmtypes.TransitGatewayRegistration,
) nmservice.TransitGatewayRegistration {
	mapped := nmservice.TransitGatewayRegistration{
		GlobalNetworkID:   strings.TrimSpace(aws.ToString(registration.GlobalNetworkId)),
		TransitGatewayARN: strings.TrimSpace(aws.ToString(registration.TransitGatewayArn)),
	}
	if state := registration.State; state != nil {
		mapped.State = strings.TrimSpace(string(state.Code))
	}
	return mapped
}

func mapCoreNetwork(core awsnmtypes.CoreNetwork) nmservice.CoreNetwork {
	mapped := nmservice.CoreNetwork{
		ARN:             strings.TrimSpace(aws.ToString(core.CoreNetworkArn)),
		ID:              strings.TrimSpace(aws.ToString(core.CoreNetworkId)),
		GlobalNetworkID: strings.TrimSpace(aws.ToString(core.GlobalNetworkId)),
		Description:     strings.TrimSpace(aws.ToString(core.Description)),
		State:           strings.TrimSpace(string(core.State)),
		CreatedAt:       aws.ToTime(core.CreatedAt),
		Tags:            mapTags(core.Tags),
	}
	for _, segment := range core.Segments {
		if name := strings.TrimSpace(aws.ToString(segment.Name)); name != "" {
			mapped.SegmentNames = append(mapped.SegmentNames, name)
		}
	}
	for _, edge := range core.Edges {
		if location := strings.TrimSpace(aws.ToString(edge.EdgeLocation)); location != "" {
			mapped.EdgeLocations = append(mapped.EdgeLocations, location)
		}
	}
	return mapped
}

func mapCoreNetworkSummary(summary awsnmtypes.CoreNetworkSummary) nmservice.CoreNetwork {
	return nmservice.CoreNetwork{
		ARN:             strings.TrimSpace(aws.ToString(summary.CoreNetworkArn)),
		ID:              strings.TrimSpace(aws.ToString(summary.CoreNetworkId)),
		GlobalNetworkID: strings.TrimSpace(aws.ToString(summary.GlobalNetworkId)),
		Description:     strings.TrimSpace(aws.ToString(summary.Description)),
		State:           strings.TrimSpace(string(summary.State)),
		Tags:            mapTags(summary.Tags),
	}
}

func mapTags(tags []awsnmtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
