// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extractor

import "time"

// RelationshipObservation is one provider-observed relationship between two GCP
// resources, identified by Cloud Asset Inventory full resource names. The
// collector preserves both full resource names, the asset types, the bounded
// relationship type, and the support state as source evidence; it resolves
// nothing and writes no graph edge.
type RelationshipObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// SourceFullResourceName is the CAI full resource name of the owning resource.
	SourceFullResourceName string
	// SourceAssetType is the CAI asset type of the source resource.
	SourceAssetType string
	// RelationshipType is the bounded provider relationship type.
	RelationshipType string
	// TargetFullResourceName is the CAI full resource name of the related
	// resource. It may be in a different project; it is preserved verbatim.
	TargetFullResourceName string
	// TargetAssetType is the CAI asset type of the target resource.
	TargetAssetType string
	// SupportState classifies completeness; blank defaults to supported.
	SupportState string
	// UpdateTime is the relationship read/update time.
	UpdateTime time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}
