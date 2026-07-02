// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
// (shard strategy, version count, and the per-version allocation percentages as
// a sorted "versionID=percentage" string slice), the version full resource names
// as correlation anchors, and one typed service_splits_traffic_to_version edge
// per allocation key. It returns the zero value gracefully for an empty
// resource.data blob and wraps a parse error for malformed JSON.
//
// traffic_allocations is a string slice rather than a nested map because the
// cloud-inventory admission/readback sanitizer preserves only scalars and string
// arrays; a nested map would be stored but silently dropped from the
// API/MCP-visible attributes.
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
		if len(data.Split.Allocations) > 0 {
			// Version IDs are control-plane identifiers and the allocation
			// percentages are posture metadata; no data-plane content or secrets
			// are present. Encode as a sorted "versionID=percentage" string slice
			// so the values survive the cloud-inventory readback sanitizer (which
			// preserves scalars and string arrays but drops nested maps). Blank
			// version IDs are skipped entirely so a stray empty key never persists
			// an attribute entry without a matching edge or inflates version_count.
			allocs := make([]string, 0, len(data.Split.Allocations))
			seen := map[string]bool{}
			for versionID, pct := range data.Split.Allocations {
				id := strings.TrimSpace(versionID)
				if id == "" {
					continue
				}
				allocs = append(allocs, id+"="+strconv.FormatFloat(pct, 'g', -1, 64))
				versionName := appEngineVersionFullName(ctx.FullResourceName, id)
				if versionName != "" && !seen[versionName] {
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
			if len(allocs) > 0 {
				sort.Strings(allocs)
				attrs["version_count"] = len(allocs)
				attrs["traffic_allocations"] = allocs
			}
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// appEngineVersionFullName builds the CAI App Engine Version full resource name
// by appending "/versions/{versionID}" to the service full resource name. Both
// inputs are trimmed and any trailing slash on the service name is dropped, so
// the returned name never carries stray whitespace or a doubled slash. It
// returns "" when either input is blank so the caller emits no edge.
func appEngineVersionFullName(serviceFullName, versionID string) string {
	svc := strings.TrimSuffix(strings.TrimSpace(serviceFullName), "/")
	id := strings.TrimSpace(versionID)
	if svc == "" || id == "" {
		return ""
	}
	return svc + "/versions/" + id
}
