// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_security.go (#6696 HK slice); uses below name catalog.X.

func init() {
	RegisterAssetExtractor(catalog.AssetTypeAPIGatewayGateway, extractAPIGateway)
}

// apiGatewayData is the bounded view of a CAI apigateway.googleapis.com/Gateway
// resource.data blob. This is the complete field set of the API Gateway v1
// Gateway resource (verified against the live REST reference for
// projects.locations.gateways: name, createTime, updateTime, labels,
// displayName, apiConfig, state, defaultHostname) — there is no additional
// field to decode. defaultHostname is a live DNS name
// ("{gatewayId}-{hash}.{region_code}.gateway.dev") and is reduced to a
// fingerprint; it is never persisted verbatim.
type apiGatewayData struct {
	CreateTime      string `json:"createTime"`
	UpdateTime      string `json:"updateTime"`
	DisplayName     string `json:"displayName"`
	APIConfig       string `json:"apiConfig"`
	State           string `json:"state"`
	DefaultHostname string `json:"defaultHostname"`
}

// extractAPIGateway extracts bounded, redaction-safe typed depth for one CAI
// API Gateway Gateway asset. It surfaces the display name, lifecycle state,
// region (derived from the Gateway's own resource-name path, since the API
// Gateway v1 resource carries no separate region field), creation/update time,
// and a fingerprint of the default hostname; it emits the
// api_gateway_uses_api_config edge to the resolved ApiConfig resource, and
// surfaces the ApiConfig full resource name as a correlation anchor only when
// apiGatewayAPIConfigFullName resolves the reference to the documented
// apigateway.googleapis.com ApiConfig shape — a malformed or wrong-domain
// apiConfig value mints no edge or anchor. The raw defaultHostname DNS name
// never leaves the parser — only its deterministic fingerprint does,
// mirroring the Pub/Sub Subscription push-endpoint host treatment.
func extractAPIGateway(ctx ExtractContext) (AttributeExtraction, error) {
	var data apiGatewayData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode api gateway data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := apiGatewayRegionFromFullName(ctx.FullResourceName); v != "" {
		attrs["region"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := normalizeRFC3339(data.UpdateTime); ok {
		attrs["update_time"] = v
	}
	if fp := pubSubPushEndpointHostFingerprint(data.DefaultHostname); fp != "" {
		attrs["default_hostname_fingerprint"] = fp
	}

	var anchors []string
	var rels []RelationshipObservation
	if target := apiGatewayAPIConfigFullName(data.APIConfig); target != "" {
		anchors = append(anchors, target)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       catalog.RelationshipTypeAPIGatewayUsesAPIConfig,
			TargetFullResourceName: target,
			TargetAssetType:        catalog.AssetTypeAPIGatewayAPIConfig,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// apiGatewayRegionFromFullName derives the Gateway's region from its own CAI
// full resource name (".../locations/<region>/gateways/<gateway>"), since the
// API Gateway v1 Gateway resource reports no separate region/location field of
// its own — the resource name is the only source of the region. It returns ""
// when the name is blank or carries no "/locations/" segment, so no region is
// fabricated for a malformed or partial name.
func apiGatewayRegionFromFullName(fullName string) string {
	trimmed := strings.TrimSpace(fullName)
	if trimmed == "" {
		return ""
	}

	idx := strings.Index(trimmed, catalog.ApiGatewayLocationsMarker)
	if idx < 0 {
		return ""
	}
	rest := trimmed[idx+len(catalog.ApiGatewayLocationsMarker):]
	region, _, ok := strings.Cut(rest, "/")
	if !ok || region == "" {
		return ""
	}
	return region
}

// apiGatewayAPIConfigFullName builds the CAI ApiConfig full resource name from
// a Gateway's apiConfig reference. The API Gateway v1 API documents apiConfig
// as the relative resource name
// "projects/{project}/locations/global/apis/{api}/configs/{apiConfig}" (per the
// projects.locations.apis.configs REST reference); the CAI full resource name
// is "//apigateway.googleapis.com/" plus that same path (per the Cloud Asset
// Inventory resource-name-format reference). This is untrusted parser input,
// so the derivation fails closed on both branches, mirroring the Org Policy
// extractor's treatment of its own untrusted resource-name input:
//
//   - An already-absolute ("//...") value is trusted as-is only when it
//     carries the exact "//apigateway.googleapis.com/" service prefix.
//     TrimPrefix would silently no-op and accept a wrong-domain absolute name
//     (e.g. a "//compute.googleapis.com/..." reference), fabricating a
//     cross-service anchor/edge from an untrusted value; any other absolute
//     value returns "".
//   - A relative value is only prefixed when it matches the documented
//     "projects/.../apis/.../configs/..." shape (recognized here by requiring
//     a non-empty "/apis/" marker with non-empty prefix and suffix); any other
//     relative value returns "" rather than minting a fabricated CAI name from
//     an arbitrary string.
func apiGatewayAPIConfigFullName(apiConfig string) string {
	trimmed := strings.TrimSpace(apiConfig)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		if strings.HasPrefix(trimmed, catalog.ApiGatewayAPIConfigFullResourceNamePrefix) {
			return trimmed
		}
		return ""
	}
	relative := strings.TrimPrefix(trimmed, "/")
	prefix, suffix, ok := strings.Cut(relative, catalog.ApiGatewayAPIConfigRelativeMarker)
	if !ok || prefix == "" || suffix == "" {
		return ""
	}
	return catalog.ApiGatewayAPIConfigFullResourceNamePrefix + relative
}
