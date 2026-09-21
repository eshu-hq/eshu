// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Network Manager metadata-only facts for one claimed account.
// Network Manager is a global service: its control plane lives in a single
// region per partition, so the SDK adapter pins that region while the scan
// boundary keeps its claimed account and region for attribution.
//
// The scanner reports global networks and their sites, devices, links,
// connections, and core networks, plus the membership, placement, device-link
// association, and transit-gateway-registration relationships AWS reports
// directly. It never mutates Network Manager state.
type Scanner struct {
	// Client is the metadata-only Network Manager snapshot source.
	Client Client
}

// Scan observes Network Manager global networks, their child resources, core
// networks, and the relationships among them through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("networkmanager scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceNetworkManager:
		boundary.ServiceKind = awscloud.ServiceNetworkManager
	default:
		return nil, fmt.Errorf("networkmanager scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Network Manager global networks: %w", err)
	}

	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, network := range snapshot.GlobalNetworks {
		next, err := globalNetworkEnvelopes(boundary, network)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, core := range snapshot.CoreNetworks {
		next, err := coreNetworkEnvelopes(boundary, core)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

// globalNetworkEnvelopes emits the global-network node and every child node and
// edge it owns.
func globalNetworkEnvelopes(boundary awscloud.Boundary, network GlobalNetwork) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(globalNetworkObservation(boundary, network))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	gnID := strings.TrimSpace(network.ID)

	for _, site := range network.Sites {
		next, err := siteEnvelopes(boundary, gnID, site)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, device := range network.Devices {
		next, err := deviceEnvelopes(boundary, gnID, device)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, link := range network.Links {
		next, err := linkEnvelopes(boundary, gnID, link)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, connection := range network.Connections {
		next, err := connectionEnvelopes(boundary, gnID, connection)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, association := range network.LinkAssociations {
		if rel := deviceUsesLinkRelationship(boundary, association); rel != nil {
			envelope, err := awscloud.NewRelationshipEnvelope(*rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, registration := range network.TransitGatewayRegistrations {
		if rel := registrationRelationship(boundary, gnID, registration); rel != nil {
			envelope, err := awscloud.NewRelationshipEnvelope(*rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func siteEnvelopes(boundary awscloud.Boundary, gnID string, site Site) ([]facts.Envelope, error) {
	site.GlobalNetworkID = preferGlobalNetworkID(site.GlobalNetworkID, gnID)
	resource, err := awscloud.NewResourceEnvelope(siteObservation(boundary, site))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	rel := parentGlobalNetworkRelationship(
		boundary,
		awscloud.RelationshipNetworkManagerSiteInGlobalNetwork,
		siteResourceID(boundary, site),
		site.ARN,
		site.GlobalNetworkID,
	)
	return appendRelationship(envelopes, rel)
}

func deviceEnvelopes(boundary awscloud.Boundary, gnID string, device Device) ([]facts.Envelope, error) {
	device.GlobalNetworkID = preferGlobalNetworkID(device.GlobalNetworkID, gnID)
	resource, err := awscloud.NewResourceEnvelope(deviceObservation(boundary, device))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, rel := range []*awscloud.RelationshipObservation{
		parentGlobalNetworkRelationship(
			boundary,
			awscloud.RelationshipNetworkManagerDeviceInGlobalNetwork,
			deviceResourceID(boundary, device),
			device.ARN,
			device.GlobalNetworkID,
		),
		deviceInSiteRelationship(boundary, device),
	} {
		var err error
		if envelopes, err = appendRelationship(envelopes, rel); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func linkEnvelopes(boundary awscloud.Boundary, gnID string, link Link) ([]facts.Envelope, error) {
	link.GlobalNetworkID = preferGlobalNetworkID(link.GlobalNetworkID, gnID)
	resource, err := awscloud.NewResourceEnvelope(linkObservation(boundary, link))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, rel := range []*awscloud.RelationshipObservation{
		parentGlobalNetworkRelationship(
			boundary,
			awscloud.RelationshipNetworkManagerLinkInGlobalNetwork,
			linkResourceID(boundary, link),
			link.ARN,
			link.GlobalNetworkID,
		),
		linkInSiteRelationship(boundary, link),
	} {
		var err error
		if envelopes, err = appendRelationship(envelopes, rel); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func connectionEnvelopes(boundary awscloud.Boundary, gnID string, connection Connection) ([]facts.Envelope, error) {
	connection.GlobalNetworkID = preferGlobalNetworkID(connection.GlobalNetworkID, gnID)
	resource, err := awscloud.NewResourceEnvelope(connectionObservation(boundary, connection))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	envelopes, err = appendRelationship(envelopes, parentGlobalNetworkRelationship(
		boundary,
		awscloud.RelationshipNetworkManagerConnectionInGlobalNetwork,
		connectionResourceID(boundary, connection),
		connection.ARN,
		connection.GlobalNetworkID,
	))
	if err != nil {
		return nil, err
	}
	for _, rel := range connectionDeviceRelationships(boundary, connection) {
		envelope, err := awscloud.NewRelationshipEnvelope(rel)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func coreNetworkEnvelopes(boundary awscloud.Boundary, core CoreNetwork) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(coreNetworkObservation(boundary, core))
	if err != nil {
		return nil, err
	}
	return appendRelationship([]facts.Envelope{resource}, coreNetworkRelationship(boundary, core))
}

// appendRelationship appends a relationship envelope when rel is non-nil,
// returning the (possibly unchanged) slice.
func appendRelationship(envelopes []facts.Envelope, rel *awscloud.RelationshipObservation) ([]facts.Envelope, error) {
	if rel == nil {
		return envelopes, nil
	}
	envelope, err := awscloud.NewRelationshipEnvelope(*rel)
	if err != nil {
		return nil, err
	}
	return append(envelopes, envelope), nil
}

// preferGlobalNetworkID returns the child-reported global network id, falling
// back to the parent's id when the child response omitted it, so parent edges
// key the correct global network even when a child record is sparse.
func preferGlobalNetworkID(childID, parentID string) string {
	if trimmed := strings.TrimSpace(childID); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(parentID)
}
