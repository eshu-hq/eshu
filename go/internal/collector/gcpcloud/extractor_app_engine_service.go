// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeAppEngineService is the CAI asset type for an App Engine Service.
const assetTypeAppEngineService = "appengine.googleapis.com/Service"

// assetTypeAppEngineVersion is the CAI asset type for an App Engine Version,
// the target of a service traffic-split edge.
const assetTypeAppEngineVersion = "appengine.googleapis.com/Version"

// relationshipTypeServiceSplitsTrafficToVersion is the bounded provider
// relationship type emitted when an App Engine Service splits traffic to one or
// more Version resources. The reducer materializes an edge only when both the
// service and the version endpoint resolve exactly within the allowed scope.
const relationshipTypeServiceSplitsTrafficToVersion = "service_splits_traffic_to_version"

func init() {
	RegisterAssetExtractor(assetTypeAppEngineService, extractAppEngineService)
}

// appEngineServiceData is the bounded view of a CAI appengine.googleapis.com/Service
// resource.data blob. Only redaction-safe control-plane traffic-split metadata
// is decoded; the service configuration (env, scaling, handlers) is not decoded.
type appEngineServiceData struct {
	ID    string `json:"id"`
	Split *struct {
		ShardBy     string             `json:"shardBy"`
		Allocations map[string]float64 `json:"allocations"`
	} `json:"split"`
}

// extractAppEngineService extracts bounded, redaction-safe typed depth for one
// App Engine Service CAI asset. It returns the service ID, traffic-split posture
// (shard strategy, version count, and the allocations map), the version full
// resource names as correlation anchors, and one typed
// service_splits_traffic_to_version edge per allocation key. It returns the zero
// value gracefully for an empty resource.data blob and wraps a parse error for
// malformed JSON.
func extractAppEngineService(ctx ExtractContext) (AttributeExtraction, error) {
	var data appEngineServiceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode app engine service data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.ID); v != "" {
		attrs["service_id"] = v
	}

	var anchors []string
	var rels []RelationshipObservation

	if data.Split != nil {
		if v := strings.TrimSpace(data.Split.ShardBy); v != "" {
			attrs["split_shard_by"] = v
		}
		if n := len(data.Split.Allocations); n > 0 {
			attrs["version_count"] = n
			// Copy the allocations map verbatim: version IDs are control-plane
			// identifiers and the allocation percentages are posture metadata; no
			// data-plane content or secrets are present.
			allocs := make(map[string]float64, n)
			seen := map[string]bool{}
			for versionID, pct := range data.Split.Allocations {
				allocs[versionID] = pct
				if versionName := appEngineVersionFullName(ctx.FullResourceName, versionID); versionName != "" && !seen[versionName] {
					seen[versionName] = true
					anchors = append(anchors, versionName)
					rels = append(rels, RelationshipObservation{
						SourceFullResourceName: ctx.FullResourceName,
						SourceAssetType:        ctx.AssetType,
						RelationshipType:       relationshipTypeServiceSplitsTrafficToVersion,
						TargetFullResourceName: versionName,
						TargetAssetType:        assetTypeAppEngineVersion,
						SupportState:           RelationshipSupportSupported,
					})
				}
			}
			attrs["traffic_allocations"] = allocs
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// appEngineVersionFullName builds the CAI App Engine Version full resource name
// by appending "/versions/{versionID}" to the service full resource name. It
// returns "" when either input is blank so the caller emits no edge.
func appEngineVersionFullName(serviceFullName, versionID string) string {
	if strings.TrimSpace(serviceFullName) == "" || strings.TrimSpace(versionID) == "" {
		return ""
	}
	return serviceFullName + "/versions/" + versionID
}
