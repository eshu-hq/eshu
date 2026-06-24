// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package outposts

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// outpostInSiteRelationship records an Outpost's membership in its parent site.
// The target is keyed by the site ARN (falling back to the short site id), which
// is the resource_id the site node publishes, so the edge joins the site node
// instead of dangling. It returns nil when either endpoint identity is missing.
func outpostInSiteRelationship(boundary awscloud.Boundary, outpost Outpost) *awscloud.RelationshipObservation {
	sourceID := outpostResourceID(outpost)
	targetID := firstNonEmpty(outpost.SiteARN, outpost.SiteID)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipOutpostsOutpostInSite,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(outpost.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeOutpostsSite,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipOutpostsOutpostInSite + ":" + targetID,
	}
}

// assetInOutpostRelationship records an asset's membership in its parent
// outpost. assetID is the resource_id the asset node publishes (its synthesized
// id under the outpost ARN), and the target is keyed by the outpost ARN
// (falling back to the short outpost id) so the edge joins the outpost node. It
// returns nil when either endpoint identity is missing.
func assetInOutpostRelationship(
	boundary awscloud.Boundary,
	outpost Outpost,
	assetID string,
) *awscloud.RelationshipObservation {
	assetID = strings.TrimSpace(assetID)
	targetID := outpostResourceID(outpost)
	if assetID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipOutpostsAssetInOutpost,
		SourceResourceID: assetID,
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeOutpostsOutpost,
		SourceRecordID:   assetID + "->" + awscloud.RelationshipOutpostsAssetInOutpost + ":" + targetID,
	}
}
