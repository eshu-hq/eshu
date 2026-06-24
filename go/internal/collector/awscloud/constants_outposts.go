// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceOutposts identifies the regional AWS Outposts metadata-only scan
	// slice. The scanner reads control-plane describe/list APIs (ListOutposts,
	// GetOutpost, ListSites, GetSite, ListAssets, ListTagsForResource) and emits
	// operational identity for outposts, sites, and rack/server assets only. It
	// never persists physical site street addresses, shipping or contact
	// details, free-form site notes, or rack physical-property logistics, and it
	// never mutates Outposts state.
	ServiceOutposts = "outposts"
)

const (
	// ResourceTypeOutpostsOutpost identifies an AWS Outposts outpost metadata
	// resource. The scanner emits identity, lifecycle status, Availability Zone,
	// owner account, supported hardware type, and the parent site id only.
	ResourceTypeOutpostsOutpost = "aws_outposts_outpost"
	// ResourceTypeOutpostsSite identifies an AWS Outposts site metadata
	// resource. The scanner emits operational identity (site id, name, account)
	// only. Physical operating addresses, free-form notes, and rack physical
	// properties are intentionally excluded from the contract.
	ResourceTypeOutpostsSite = "aws_outposts_site"
	// ResourceTypeOutpostsAsset identifies an AWS Outposts asset (a rack server
	// or server-form-factor unit) metadata resource. The scanner emits the asset
	// id, asset type, rack id, compute lifecycle state, and rack elevation only.
	ResourceTypeOutpostsAsset = "aws_outposts_asset"
)

const (
	// RelationshipOutpostsOutpostInSite records an Outposts outpost's membership
	// in its parent site. The target is keyed by the site ARN so the edge joins
	// the site node the scanner publishes.
	RelationshipOutpostsOutpostInSite = "outposts_outpost_in_site"
	// RelationshipOutpostsAssetInOutpost records an Outposts asset's membership
	// in its parent outpost. The target is keyed by the outpost ARN so the edge
	// joins the outpost node the scanner publishes.
	RelationshipOutpostsAssetInOutpost = "outposts_asset_in_outpost"
)
