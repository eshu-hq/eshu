// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AttributeExtraction is the bounded, redaction-safe typed-depth output of a
// per-asset-type extractor. It carries:
//
//   - Attributes: a bounded map of type-specific control-plane fields usable for
//     Terraform import/drift, edges, correlation, or monitoring. It never holds
//     secrets, data-plane content, raw policy JSON, IPs, or response bodies.
//   - CorrelationAnchors: cross-source join keys (KMS key names, parent resource
//     names, and similar resource identifiers) for downstream correlation.
//   - Relationships: typed provider relationship observations whose source and
//     target are CAI full resource names; reducers resolve and materialize them.
//
// An extractor that finds no usable typed depth returns the zero value; nil
// maps and slices are valid and mean "nothing to add".
type AttributeExtraction struct {
	Attributes         map[string]any
	CorrelationAnchors []string
	Relationships      []RelationshipObservation
}

// ExtractContext is the bounded input handed to a per-asset-type extractor. Data
// is the raw CAI resource.data JSON for one asset; it is never persisted and the
// extractor must pull only redaction-safe fields from it. FullResourceName,
// AssetType, and ProjectID are the already-normalized resource identity the
// extractor uses to build typed relationship endpoints and anchors.
type ExtractContext struct {
	FullResourceName string
	AssetType        string
	ProjectID        string
	Data             json.RawMessage
}

// AssetAttributeExtractor extracts bounded typed depth from one CAI resource
// data blob for a single asset type. Each supported asset type registers its own
// extractor in its own file so parallel type additions never collide in a shared
// parser switch. An extractor must be deterministic and side-effect free, and it
// must never return secrets, data-plane content, or raw provider bodies.
type AssetAttributeExtractor func(ctx ExtractContext) (AttributeExtraction, error)

// assetExtractors is the per-asset-type extractor registry. It is populated only
// from package init functions (one per asset-type file), so it is effectively
// immutable after program initialization and needs no lock for lookup.
var assetExtractors = map[string]AssetAttributeExtractor{}

// RegisterAssetExtractor installs the extractor for one CAI asset type. It is
// intended to be called from a package init function in the asset type's own
// file. It panics on a blank asset type, a nil extractor, or a duplicate
// registration so a wiring mistake fails loudly at startup rather than silently
// dropping typed depth or shadowing another type's extractor.
func RegisterAssetExtractor(assetType string, extractor AssetAttributeExtractor) {
	trimmed := strings.TrimSpace(assetType)
	if trimmed == "" {
		panic("gcpcloud: RegisterAssetExtractor requires a non-blank asset type")
	}
	if extractor == nil {
		panic(fmt.Sprintf("gcpcloud: RegisterAssetExtractor for %q requires a non-nil extractor", trimmed))
	}
	if _, exists := assetExtractors[trimmed]; exists {
		panic(fmt.Sprintf("gcpcloud: duplicate extractor registration for asset type %q", trimmed))
	}
	assetExtractors[trimmed] = extractor
}

// lookupAssetExtractor returns the registered extractor for an asset type.
func lookupAssetExtractor(assetType string) (AssetAttributeExtractor, bool) {
	extractor, ok := assetExtractors[strings.TrimSpace(assetType)]
	return extractor, ok
}

// extractAssetAttributes dispatches to the registered extractor for the context
// asset type. It returns handled=false (with no error) when no extractor is
// registered, so the parser keeps emitting the bounded base observation for
// asset types without typed depth. A registered extractor's error is wrapped so
// the caller can attribute it to the asset type without leaking resource data.
func extractAssetAttributes(ctx ExtractContext) (AttributeExtraction, bool, error) {
	extractor, ok := lookupAssetExtractor(ctx.AssetType)
	if !ok {
		return AttributeExtraction{}, false, nil
	}
	extraction, err := extractor(ctx)
	if err != nil {
		return AttributeExtraction{}, true, fmt.Errorf("extract %s attributes: %w", strings.TrimSpace(ctx.AssetType), err)
	}
	return extraction, true, nil
}

// unregisterAssetExtractorForTest removes a registration so registry unit tests
// can register sentinel asset types without leaking state across test cases. It
// is test-only and must not be called from production code paths.
func unregisterAssetExtractorForTest(assetType string) {
	delete(assetExtractors, strings.TrimSpace(assetType))
}
