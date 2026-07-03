// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeSpannerInstance is the Cloud Asset Inventory asset type for a Cloud
// Spanner Instance.
const assetTypeSpannerInstance = "spanner.googleapis.com/Instance"

func init() {
	RegisterAssetExtractor(assetTypeSpannerInstance, extractSpannerInstance)
}

// spannerInstanceData is the bounded view of a CAI spanner.googleapis.com/Instance
// resource.data blob. Only control-plane metadata is decoded. NodeCount and
// ProcessingUnits are *int64 so a genuinely absent field (an instance
// provisioned by the other capacity mode) is distinguishable from an explicit
// zero, which Spanner never reports for a real instance and would otherwise
// fabricate a posture from a sparse CAI page. Labels are decoded only to their
// map length: raw label keys/values are redaction-safe fingerprinting handled
// by the base observation path in parse.go, never by this typed-depth
// extractor. EndpointUris (data-plane connection endpoints) are intentionally
// not declared as a struct field at all, so they are never decoded into Go
// memory in the first place.
type spannerInstanceData struct {
	Config          string            `json:"config"`
	DisplayName     string            `json:"displayName"`
	NodeCount       *int64            `json:"nodeCount"`
	ProcessingUnits *int64            `json:"processingUnits"`
	State           string            `json:"state"`
	Labels          map[string]string `json:"labels"`
}

// extractSpannerInstance extracts bounded, redaction-safe typed depth for one
// Cloud Spanner Instance CAI asset. It returns the Terraform/drift/monitoring
// attribute set (instance config short name, display name, node count or
// processing units — an instance is provisioned by exactly one of the two
// capacity modes — lifecycle state, and a bounded label count). Cloud Spanner
// reports no resolvable CAI edge target on the Instance resource itself: the
// instance config is a regional/multi-region topology descriptor rather than a
// separate CAI asset, and CMEK is a per-Database property (encryptionConfig)
// carried by the child spanner.googleapis.com/Database asset type, not by the
// Instance. Raw label keys/values, and any data-plane connection endpoint, are
// never decoded here.
func extractSpannerInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data spannerInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode spanner instance data: %w", err)
	}

	return AttributeExtraction{
		Attributes: spannerInstanceAttributes(data),
	}, nil
}

// spannerInstanceAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture.
func spannerInstanceAttributes(data spannerInstanceData) map[string]any {
	attrs := map[string]any{}
	if v := spannerInstanceConfigShortName(data.Config); v != "" {
		attrs["config"] = v
	}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if data.NodeCount != nil && *data.NodeCount > 0 {
		attrs["node_count"] = *data.NodeCount
	}
	if data.ProcessingUnits != nil && *data.ProcessingUnits > 0 {
		attrs["processing_units"] = *data.ProcessingUnits
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if len(data.Labels) > 0 {
		attrs["label_count"] = len(data.Labels)
	}
	return attrs
}

// spannerInstanceConfigShortName reduces a Spanner instance config reference
// ("projects/<p>/instanceConfigs/<config-id>", or a bare config id on a sparse
// CAI page) to its trailing config id segment — the regional or multi-region
// topology name (for example "regional-us-central1" or "nam-eur-asia1"). It
// returns "" for a blank reference so the caller omits rather than fabricates
// the attribute.
func spannerInstanceConfigShortName(config string) string {
	trimmed := strings.TrimSpace(config)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx+1 < len(trimmed) {
		return trimmed[idx+1:]
	}
	return trimmed
}
