// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package outposts

import (
	"strings"
)

// outpostResourceID returns the resource_id the outpost node publishes. It
// prefers the outpost ARN (always present from ListOutposts) and falls back to
// the short outpost id, so asset-in-outpost edges can key the outpost by the
// same value the node publishes.
func outpostResourceID(outpost Outpost) string {
	return firstNonEmpty(outpost.ARN, outpost.OutpostID)
}

// siteResourceID returns the resource_id the site node publishes. It prefers the
// site ARN (always present from ListSites) and falls back to the short site id,
// so the outpost-in-site edge can key the site by the same value the node
// publishes.
func siteResourceID(site Site) string {
	return firstNonEmpty(site.ARN, site.SiteID)
}

// assetResourceID returns the stable resource_id for an Outposts asset. AWS does
// not expose an asset ARN, so the scanner synthesizes one under the parent
// outpost ARN (arn:<partition>:...:outpost/<id>/asset/<assetID>). Building it
// from the outpost ARN keeps the id partition-aware in every partition without
// concatenating a literal arn:aws: prefix; it falls back to a bare
// <outpostID>/asset/<assetID> form when the parent outpost has no ARN.
func assetResourceID(outpost Outpost, asset Asset) string {
	assetID := strings.TrimSpace(asset.AssetID)
	if assetID == "" {
		return ""
	}
	parent := outpostResourceID(outpost)
	if parent == "" {
		return ""
	}
	return parent + "/asset/" + assetID
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
