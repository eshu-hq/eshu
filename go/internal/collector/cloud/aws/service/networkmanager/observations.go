// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkmanager

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// globalNetworkObservation builds the resource observation for a global network.
// The node publishes its resource_id as the API-reported ARN (synthesized from
// the boundary account when a fixture omits it).
func globalNetworkObservation(boundary aws.Boundary, network GlobalNetwork) aws.ResourceObservation {
	resourceID := globalNetworkResourceID(boundary, network)
	id := strings.TrimSpace(network.ID)
	arn := strings.TrimSpace(network.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeNetworkManagerGlobalNetwork,
		Name:         id,
		State:        strings.TrimSpace(network.State),
		Tags:         cloneStringMap(network.Tags),
		Attributes: map[string]any{
			"global_network_id": id,
			"description":       strings.TrimSpace(network.Description),
			"created_at":        timeOrNil(network.CreatedAt),
		},
		CorrelationAnchors: anchors(resourceID, arn, id),
		SourceRecordID:     resourceID,
	}
}

// siteObservation builds the resource observation for a site.
func siteObservation(boundary aws.Boundary, site Site) aws.ResourceObservation {
	resourceID := siteResourceID(boundary, site)
	id := strings.TrimSpace(site.ID)
	arn := strings.TrimSpace(site.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeNetworkManagerSite,
		Name:         id,
		State:        strings.TrimSpace(site.State),
		Tags:         cloneStringMap(site.Tags),
		Attributes: map[string]any{
			"site_id":           id,
			"global_network_id": strings.TrimSpace(site.GlobalNetworkID),
			"description":       strings.TrimSpace(site.Description),
			"address":           strings.TrimSpace(site.Address),
			"latitude":          strings.TrimSpace(site.Latitude),
			"longitude":         strings.TrimSpace(site.Longitude),
			"created_at":        timeOrNil(site.CreatedAt),
		},
		CorrelationAnchors: anchors(resourceID, arn, id),
		SourceRecordID:     resourceID,
	}
}

// deviceObservation builds the resource observation for a device.
func deviceObservation(boundary aws.Boundary, device Device) aws.ResourceObservation {
	resourceID := deviceResourceID(boundary, device)
	id := strings.TrimSpace(device.ID)
	arn := strings.TrimSpace(device.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeNetworkManagerDevice,
		Name:         id,
		State:        strings.TrimSpace(device.State),
		Tags:         cloneStringMap(device.Tags),
		Attributes: map[string]any{
			"device_id":         id,
			"global_network_id": strings.TrimSpace(device.GlobalNetworkID),
			"site_id":           strings.TrimSpace(device.SiteID),
			"description":       strings.TrimSpace(device.Description),
			"type":              strings.TrimSpace(device.Type),
			"vendor":            strings.TrimSpace(device.Vendor),
			"model":             strings.TrimSpace(device.Model),
			"subnet_arn":        strings.TrimSpace(device.SubnetARN),
			"zone":              strings.TrimSpace(device.Zone),
			"address":           strings.TrimSpace(device.Address),
			"latitude":          strings.TrimSpace(device.Latitude),
			"longitude":         strings.TrimSpace(device.Longitude),
			"created_at":        timeOrNil(device.CreatedAt),
		},
		CorrelationAnchors: anchors(resourceID, arn, id),
		SourceRecordID:     resourceID,
	}
}

// linkObservation builds the resource observation for a link.
func linkObservation(boundary aws.Boundary, link Link) aws.ResourceObservation {
	resourceID := linkResourceID(boundary, link)
	id := strings.TrimSpace(link.ID)
	arn := strings.TrimSpace(link.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeNetworkManagerLink,
		Name:         id,
		State:        strings.TrimSpace(link.State),
		Tags:         cloneStringMap(link.Tags),
		Attributes: map[string]any{
			"link_id":             id,
			"global_network_id":   strings.TrimSpace(link.GlobalNetworkID),
			"site_id":             strings.TrimSpace(link.SiteID),
			"description":         strings.TrimSpace(link.Description),
			"type":                strings.TrimSpace(link.Type),
			"provider":            strings.TrimSpace(link.Provider),
			"upload_speed_mbps":   link.UploadSpeedMbps,
			"download_speed_mbps": link.DownloadSpeedMbps,
			"created_at":          timeOrNil(link.CreatedAt),
		},
		CorrelationAnchors: anchors(resourceID, arn, id),
		SourceRecordID:     resourceID,
	}
}

// connectionObservation builds the resource observation for a connection.
func connectionObservation(boundary aws.Boundary, connection Connection) aws.ResourceObservation {
	resourceID := connectionResourceID(boundary, connection)
	id := strings.TrimSpace(connection.ID)
	arn := strings.TrimSpace(connection.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeNetworkManagerConnection,
		Name:         id,
		State:        strings.TrimSpace(connection.State),
		Tags:         cloneStringMap(connection.Tags),
		Attributes: map[string]any{
			"connection_id":       id,
			"global_network_id":   strings.TrimSpace(connection.GlobalNetworkID),
			"device_id":           strings.TrimSpace(connection.DeviceID),
			"connected_device_id": strings.TrimSpace(connection.ConnectedDeviceID),
			"link_id":             strings.TrimSpace(connection.LinkID),
			"connected_link_id":   strings.TrimSpace(connection.ConnectedLinkID),
			"description":         strings.TrimSpace(connection.Description),
			"created_at":          timeOrNil(connection.CreatedAt),
		},
		CorrelationAnchors: anchors(resourceID, arn, id),
		SourceRecordID:     resourceID,
	}
}

// coreNetworkObservation builds the resource observation for a core network.
func coreNetworkObservation(boundary aws.Boundary, core CoreNetwork) aws.ResourceObservation {
	resourceID := coreNetworkResourceID(boundary, core)
	id := strings.TrimSpace(core.ID)
	arn := strings.TrimSpace(core.ARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeNetworkManagerCoreNetwork,
		Name:         id,
		State:        strings.TrimSpace(core.State),
		Tags:         cloneStringMap(core.Tags),
		Attributes: map[string]any{
			"core_network_id":   id,
			"global_network_id": strings.TrimSpace(core.GlobalNetworkID),
			"description":       strings.TrimSpace(core.Description),
			"segment_names":     cloneStrings(core.SegmentNames),
			"edge_locations":    cloneStrings(core.EdgeLocations),
			"created_at":        timeOrNil(core.CreatedAt),
		},
		CorrelationAnchors: anchors(resourceID, arn, id),
		SourceRecordID:     resourceID,
	}
}

// globalNetworkChildResourceID returns the resource_id for a child resource that
// publishes its API-reported ARN, falling back to a synthesized partition-aware
// ARN built from synth when AWS omitted it (only in tests). It returns "" when
// neither is available.
func globalNetworkChildResourceID(apiARN, synth string) string {
	if arn := strings.TrimSpace(apiARN); arn != "" {
		return arn
	}
	return strings.TrimSpace(synth)
}

func siteResourceID(boundary aws.Boundary, site Site) string {
	return globalNetworkChildResourceID(site.ARN, siteARN(boundary, site.GlobalNetworkID, site.ID))
}

func deviceResourceID(boundary aws.Boundary, device Device) string {
	return globalNetworkChildResourceID(device.ARN, deviceARN(boundary, device.GlobalNetworkID, device.ID))
}

func linkResourceID(boundary aws.Boundary, link Link) string {
	return globalNetworkChildResourceID(link.ARN, linkARN(boundary, link.GlobalNetworkID, link.ID))
}

func connectionResourceID(boundary aws.Boundary, connection Connection) string {
	synth := networkManagerARN(boundary, "connection/"+strings.TrimSpace(connection.GlobalNetworkID)+"/"+strings.TrimSpace(connection.ID))
	if strings.TrimSpace(connection.GlobalNetworkID) == "" || strings.TrimSpace(connection.ID) == "" {
		synth = ""
	}
	return globalNetworkChildResourceID(connection.ARN, synth)
}

func coreNetworkResourceID(boundary aws.Boundary, core CoreNetwork) string {
	synth := networkManagerARN(boundary, "core-network/"+strings.TrimSpace(core.ID))
	if strings.TrimSpace(core.ID) == "" {
		synth = ""
	}
	return globalNetworkChildResourceID(core.ARN, synth)
}

// anchors returns the de-duplicated non-empty correlation anchors for a node.
func anchors(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
