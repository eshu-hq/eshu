// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkmanager

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// parentGlobalNetworkRelationship records a child resource's membership in its
// parent global network. The target is keyed by the parent global-network ARN,
// the resource_id the global-network node publishes, so the edge joins it. It
// returns nil when either endpoint identity is missing.
func parentGlobalNetworkRelationship(
	boundary aws.Boundary,
	relationshipType, sourceID, sourceARN, globalNetworkID string,
) *aws.RelationshipObservation {
	sourceID = strings.TrimSpace(sourceID)
	parentARN := globalNetworkARN(boundary, globalNetworkID)
	if sourceID == "" || parentARN == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(sourceARN),
		TargetResourceID: parentARN,
		TargetARN:        parentARN,
		TargetType:       aws.ResourceTypeNetworkManagerGlobalNetwork,
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + parentARN,
	}
}

// deviceInSiteRelationship records a device's placement at a site, keyed by the
// site ARN the site node publishes. It returns nil when the device reports no
// site or either endpoint identity is missing.
func deviceInSiteRelationship(boundary aws.Boundary, device Device) *aws.RelationshipObservation {
	sourceID := deviceResourceID(boundary, device)
	targetARN := siteARN(boundary, device.GlobalNetworkID, device.SiteID)
	if sourceID == "" || targetARN == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipNetworkManagerDeviceInSite,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(device.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeNetworkManagerSite,
		SourceRecordID:   sourceID + "->" + aws.RelationshipNetworkManagerDeviceInSite + ":" + targetARN,
	}
}

// linkInSiteRelationship records a link's placement at a site, keyed by the site
// ARN the site node publishes. It returns nil when the link reports no site or
// either endpoint identity is missing.
func linkInSiteRelationship(boundary aws.Boundary, link Link) *aws.RelationshipObservation {
	sourceID := linkResourceID(boundary, link)
	targetARN := siteARN(boundary, link.GlobalNetworkID, link.SiteID)
	if sourceID == "" || targetARN == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipNetworkManagerLinkInSite,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(link.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeNetworkManagerSite,
		SourceRecordID:   sourceID + "->" + aws.RelationshipNetworkManagerLinkInSite + ":" + targetARN,
	}
}

// deviceUsesLinkRelationship records a device-to-link association reported by
// GetLinkAssociations, keyed by the link ARN the link node publishes. It returns
// nil when either endpoint identity is missing.
func deviceUsesLinkRelationship(
	boundary aws.Boundary,
	association LinkAssociation,
) *aws.RelationshipObservation {
	sourceARN := deviceARN(boundary, association.GlobalNetworkID, association.DeviceID)
	targetARN := linkARN(boundary, association.GlobalNetworkID, association.LinkID)
	if sourceARN == "" || targetARN == "" {
		return nil
	}
	rel := &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipNetworkManagerDeviceUsesLink,
		SourceResourceID: sourceARN,
		SourceARN:        sourceARN,
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeNetworkManagerLink,
		SourceRecordID:   sourceARN + "->" + aws.RelationshipNetworkManagerDeviceUsesLink + ":" + targetARN,
	}
	if state := strings.TrimSpace(association.State); state != "" {
		rel.Attributes = map[string]any{"association_state": state}
	}
	return rel
}

// connectionDeviceRelationships records a connection's references to its two
// endpoint devices, each keyed by the device ARN the device node publishes. A
// connection always names a first device and may name a connected second device.
func connectionDeviceRelationships(
	boundary aws.Boundary,
	connection Connection,
) []aws.RelationshipObservation {
	sourceID := connectionResourceID(boundary, connection)
	if sourceID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	for _, deviceID := range []string{connection.DeviceID, connection.ConnectedDeviceID} {
		targetARN := deviceARN(boundary, connection.GlobalNetworkID, deviceID)
		if targetARN == "" {
			continue
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipNetworkManagerConnectionConnectsDevice,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(connection.ARN),
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeNetworkManagerDevice,
			SourceRecordID:   sourceID + "->" + aws.RelationshipNetworkManagerConnectionConnectsDevice + ":" + targetARN,
		})
	}
	return observations
}

// registrationRelationship records a transit gateway's registration into a
// global network. The source is the global-network ARN the global-network node
// publishes; the target is keyed by the bare transit gateway id the transit
// gateway node publishes, extracted from the reported ARN. It returns nil when
// either endpoint identity is missing.
func registrationRelationship(
	boundary aws.Boundary,
	globalNetworkID string,
	registration TransitGatewayRegistration,
) *aws.RelationshipObservation {
	sourceARN := globalNetworkARN(boundary, globalNetworkID)
	tgwID := transitGatewayID(registration.TransitGatewayARN)
	if sourceARN == "" || tgwID == "" {
		return nil
	}
	rel := &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipNetworkManagerGlobalNetworkRegistersTransitGateway,
		SourceResourceID: sourceARN,
		SourceARN:        sourceARN,
		TargetResourceID: tgwID,
		TargetType:       aws.ResourceTypeTransitGateway,
		SourceRecordID:   sourceARN + "->" + aws.RelationshipNetworkManagerGlobalNetworkRegistersTransitGateway + ":" + tgwID,
	}
	if tgwARN := strings.TrimSpace(registration.TransitGatewayARN); isARN(tgwARN) {
		rel.Attributes = map[string]any{"transit_gateway_arn": tgwARN}
	}
	if state := strings.TrimSpace(registration.State); state != "" {
		if rel.Attributes == nil {
			rel.Attributes = map[string]any{}
		}
		rel.Attributes["registration_state"] = state
	}
	return rel
}

// coreNetworkRelationship records a core network's membership in its parent
// global network, keyed by the parent global-network ARN. It returns nil when
// either endpoint identity is missing.
func coreNetworkRelationship(boundary aws.Boundary, core CoreNetwork) *aws.RelationshipObservation {
	return parentGlobalNetworkRelationship(
		boundary,
		aws.RelationshipNetworkManagerCoreNetworkInGlobalNetwork,
		coreNetworkResourceID(boundary, core),
		core.ARN,
		core.GlobalNetworkID,
	)
}
