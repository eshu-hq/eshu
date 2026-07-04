// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeAPIGatewayGateway is the Cloud Asset Inventory asset type for a GCP
// API Gateway Gateway.
const assetTypeAPIGatewayGateway = "apigateway.googleapis.com/Gateway"

// assetTypeAPIGatewayAPIConfig is the Cloud Asset Inventory asset type for a
// GCP API Gateway ApiConfig. ApiConfig is itself a separately CAI-inventoried
// asset type (verified against the live Cloud Asset Inventory supported-asset-
// types reference), so a Gateway's apiConfig reference resolves to a real edge
// endpoint rather than staying an attribute-only value.
const assetTypeAPIGatewayAPIConfig = "apigateway.googleapis.com/ApiConfig"

// relationshipTypeAPIGatewayUsesAPIConfig is the bounded provider relationship
// type for a Gateway's required apiConfig reference, carried on a
// gcp_cloud_relationship fact. The reducer materializes the edge only when both
// endpoints resolve exactly.
const relationshipTypeAPIGatewayUsesAPIConfig = "api_gateway_uses_api_config"

// apiGatewayAPIConfigFullResourceNamePrefix is the exact CAI full-resource-name
// service prefix for an apigateway.googleapis.com/ApiConfig, verified against
// the live Cloud Asset Inventory resource-name-format reference:
// "//apigateway.googleapis.com/projects/{project}/locations/{location}/apis/{api}/configs/{apiConfig}".
// An already-absolute apiConfig value must carry this exact prefix to be
// trusted as-is; any other absolute value names a different, untrusted
// service and must not mint an edge or anchor.
const apiGatewayAPIConfigFullResourceNamePrefix = "//apigateway.googleapis.com/"

// apiGatewayAPIConfigRelativeMarker is the fixed "/apis/" path segment in the
// documented relative ApiConfig resource-name shape
// ("projects/{project}/locations/global/apis/{api}/configs/{apiConfig}", per
// the API Gateway v1 projects.locations.apis.configs REST reference). A
// relative apiConfig value must contain this marker with a non-empty prefix
// and suffix to be recognized as that documented shape.
const apiGatewayAPIConfigRelativeMarker = "/apis/"

func init() {
	RegisterAssetExtractor(assetTypeAPIGatewayGateway, extractAPIGateway)
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
			RelationshipType:       relationshipTypeAPIGatewayUsesAPIConfig,
			TargetFullResourceName: target,
			TargetAssetType:        assetTypeAPIGatewayAPIConfig,
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
	const marker = "/locations/"
	idx := strings.Index(trimmed, marker)
	if idx < 0 {
		return ""
	}
	rest := trimmed[idx+len(marker):]
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
		if strings.HasPrefix(trimmed, apiGatewayAPIConfigFullResourceNamePrefix) {
			return trimmed
		}
		return ""
	}
	relative := strings.TrimPrefix(trimmed, "/")
	prefix, suffix, ok := strings.Cut(relative, apiGatewayAPIConfigRelativeMarker)
	if !ok || prefix == "" || suffix == "" {
		return ""
	}
	return apiGatewayAPIConfigFullResourceNamePrefix + relative
}
