// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package outposts

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Outposts metadata-only facts for one claimed account and
// region. It reports outposts, sites, and rack/server assets plus the
// outpost-in-site and asset-in-outpost membership relationships. It never reads
// or persists physical site street addresses, shipping or contact details,
// free-form site notes, or rack physical-property logistics, and it never
// mutates Outposts state.
type Scanner struct {
	// Client is the metadata-only Outposts snapshot source.
	Client Client
}

// Scan observes Outposts outposts, their installed assets, and the regional
// sites through the configured client, emitting resource and relationship facts.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("outposts scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceOutposts:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceOutposts
	default:
		return nil, fmt.Errorf("outposts scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Outposts metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, site := range snapshot.Sites {
		envelope, err := aws.NewResourceEnvelope(siteObservation(boundary, site))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, outpost := range snapshot.Outposts {
		next, err := outpostEnvelopes(boundary, outpost)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func outpostEnvelopes(boundary aws.Boundary, outpost Outpost) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(outpostObservation(boundary, outpost))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := outpostInSiteRelationship(boundary, outpost); relationship != nil {
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, asset := range outpost.Assets {
		next, err := assetEnvelopes(boundary, outpost, asset)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func assetEnvelopes(boundary aws.Boundary, outpost Outpost, asset Asset) ([]facts.Envelope, error) {
	assetID := assetResourceID(outpost, asset)
	if assetID == "" {
		return nil, nil
	}
	resource, err := aws.NewResourceEnvelope(assetObservation(boundary, outpost, asset, assetID))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := assetInOutpostRelationship(boundary, outpost, assetID); relationship != nil {
		envelope, err := aws.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func outpostObservation(boundary aws.Boundary, outpost Outpost) aws.ResourceObservation {
	outpostARN := strings.TrimSpace(outpost.ARN)
	name := strings.TrimSpace(outpost.Name)
	resourceID := outpostResourceID(outpost)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          outpostARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeOutpostsOutpost,
		Name:         name,
		State:        strings.TrimSpace(outpost.LifeCycleStatus),
		Tags:         cloneStringMap(outpost.Tags),
		Attributes: map[string]any{
			"outpost_id":              strings.TrimSpace(outpost.OutpostID),
			"description":             strings.TrimSpace(outpost.Description),
			"availability_zone":       strings.TrimSpace(outpost.AvailabilityZone),
			"availability_zone_id":    strings.TrimSpace(outpost.AvailabilityZoneID),
			"owner_id":                strings.TrimSpace(outpost.OwnerID),
			"site_id":                 strings.TrimSpace(outpost.SiteID),
			"supported_hardware_type": strings.TrimSpace(outpost.SupportedHardwareType),
		},
		CorrelationAnchors: []string{outpostARN, strings.TrimSpace(outpost.OutpostID), name},
		SourceRecordID:     resourceID,
	}
}

func siteObservation(boundary aws.Boundary, site Site) aws.ResourceObservation {
	siteARN := strings.TrimSpace(site.ARN)
	name := strings.TrimSpace(site.Name)
	resourceID := siteResourceID(site)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          siteARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeOutpostsSite,
		Name:         name,
		Tags:         cloneStringMap(site.Tags),
		Attributes: map[string]any{
			"site_id":    strings.TrimSpace(site.SiteID),
			"account_id": strings.TrimSpace(site.AccountID),
		},
		CorrelationAnchors: []string{siteARN, strings.TrimSpace(site.SiteID), name},
		SourceRecordID:     resourceID,
	}
}

func assetObservation(
	boundary aws.Boundary,
	outpost Outpost,
	asset Asset,
	assetID string,
) aws.ResourceObservation {
	attributes := map[string]any{
		"asset_id":   strings.TrimSpace(asset.AssetID),
		"asset_type": strings.TrimSpace(asset.AssetType),
		"rack_id":    strings.TrimSpace(asset.RackID),
		"outpost_id": strings.TrimSpace(outpost.OutpostID),
	}
	if asset.RackElevation != nil {
		attributes["rack_elevation"] = *asset.RackElevation
	}
	return aws.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         assetID,
		ResourceType:       aws.ResourceTypeOutpostsAsset,
		Name:               strings.TrimSpace(asset.AssetID),
		State:              strings.TrimSpace(asset.ComputeState),
		Attributes:         attributes,
		CorrelationAnchors: []string{assetID, strings.TrimSpace(asset.AssetID)},
		SourceRecordID:     assetID,
	}
}
