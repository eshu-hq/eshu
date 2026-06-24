// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package outposts

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Outposts observations for one AWS claim.
// Implementations read control-plane describe/list APIs only and never persist
// physical site addresses, shipping or contact details, free-form site notes,
// or rack physical-property logistics.
type Client interface {
	// Snapshot returns every Outpost, site, and asset visible to the configured
	// AWS credentials, each carrying only operational identity metadata.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Outposts control-plane metadata plus non-fatal scan
// warnings. Outposts and Sites are independent top-level collections; assets
// hang under their parent outpost.
type Snapshot struct {
	// Outposts is the metadata-only set of Outposts, each carrying its assets.
	Outposts []Outpost
	// Sites is the metadata-only set of Outposts sites. Only operational
	// identity is carried; physical addresses and notes are never modeled.
	Sites []Site
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Outpost is the scanner-owned AWS Outposts outpost model. It carries
// control-plane operational identity only.
type Outpost struct {
	// ARN is the Amazon Resource Name that uniquely identifies the outpost.
	ARN string
	// OutpostID is the short Outpost id (op-...).
	OutpostID string
	// Name is the outpost name.
	Name string
	// Description is the operator-supplied outpost description.
	Description string
	// LifeCycleStatus is the reported outpost lifecycle status (for example
	// ACTIVE).
	LifeCycleStatus string
	// AvailabilityZone is the AWS Availability Zone the outpost maps to.
	AvailabilityZone string
	// AvailabilityZoneID is the AWS Availability Zone id the outpost maps to.
	AvailabilityZoneID string
	// OwnerID is the AWS account id that owns the outpost.
	OwnerID string
	// SiteID is the short id of the parent site.
	SiteID string
	// SiteARN is the ARN of the parent site, used to key the outpost-in-site
	// edge to the site node's published resource_id.
	SiteARN string
	// SupportedHardwareType is the outpost hardware form factor (RACK or
	// SERVER).
	SupportedHardwareType string
	// Tags carries the outpost resource tags.
	Tags map[string]string
	// Assets are the metadata-only assets installed under this outpost.
	Assets []Asset
}

// Site is the scanner-owned AWS Outposts site model. It intentionally carries
// operational identity ONLY. Physical operating addresses, the ISO country
// code, free-form notes, and rack physical properties returned by the AWS API
// are never copied here, so they can never reach a fact payload.
type Site struct {
	// ARN is the Amazon Resource Name that uniquely identifies the site.
	ARN string
	// SiteID is the short site id (os-...).
	SiteID string
	// Name is the site name.
	Name string
	// AccountID is the AWS account id that owns the site.
	AccountID string
	// Tags carries the site resource tags.
	Tags map[string]string
}

// Asset is the scanner-owned AWS Outposts asset model. An asset is a single
// rack server or a server-form-factor unit. It carries the asset id, type,
// parent rack, and compute lifecycle state only.
type Asset struct {
	// AssetID is the Outposts asset id.
	AssetID string
	// AssetType is the asset type (for example COMPUTE).
	AssetType string
	// RackID is the id of the rack the asset is installed in.
	RackID string
	// ComputeState is the compute lifecycle state (ACTIVE, ISOLATED, RETIRING,
	// INSTALLING) reported for a compute asset, when present.
	ComputeState string
	// RackElevation is the asset's position in the rack measured in rack units,
	// when reported. It is operational placement metadata, not a physical
	// address.
	RackElevation *float64
}
