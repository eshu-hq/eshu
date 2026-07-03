// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeSpannerInstance is the Cloud Asset Inventory asset type for a Cloud
// Spanner Instance. assetTypeSpannerInstanceConfig is the CAI asset type for
// the instance's regional/multi-region topology config, a separately
// CAI-inventoried resource (per the Cloud Asset Inventory supported-asset-types
// list), which is why the config reference resolves to a real typed edge rather
// than staying an opaque attribute.
const (
	assetTypeSpannerInstance       = "spanner.googleapis.com/Instance"
	assetTypeSpannerInstanceConfig = "spanner.googleapis.com/InstanceConfig"
)

// spannerResourceNamePrefix is the CAI full-resource-name prefix for Cloud
// Spanner resources, used to build the InstanceConfig edge endpoint from the
// instance's `config` reference (a "projects/<p>/instanceConfigs/<id>" relative
// name in the Spanner API).
const spannerResourceNamePrefix = "//spanner.googleapis.com/"

// relationshipTypeSpannerInstanceUsesInstanceConfig is the bounded provider
// relationship type carried on a gcp_cloud_relationship fact for the edge from
// a Spanner Instance to the InstanceConfig that defines its regional or
// multi-region topology. The reducer materializes the edge only when both
// endpoints resolve exactly.
const relationshipTypeSpannerInstanceUsesInstanceConfig = "spanner_instance_uses_instance_config"

func init() {
	RegisterAssetExtractor(assetTypeSpannerInstance, extractSpannerInstance)
}

// spannerInstanceData is the bounded view of a CAI spanner.googleapis.com/Instance
// resource.data blob. Only control-plane metadata is decoded. NodeCount and
// ProcessingUnits are *int64 so a genuinely absent field (an instance
// provisioned by the other capacity mode) is distinguishable from an explicit
// zero. Zero is a legitimate reported value here, not a sparse-page artifact:
// the Spanner projects.instances REST resource reports nodeCount/processingUnits
// as 0 for a FREE_INSTANCE (the free tier has no provisioned compute capacity)
// and can report 0 for a standard instance still in the CREATING state before
// capacity is assigned, so the nil-vs-zero distinction is preserved end to end
// rather than collapsed — nil omits, an explicit zero is kept. Labels are
// decoded as map[string]json.RawMessage so only the label *keys* are
// materialized into Go strings (to count them); the raw label *values* are
// left as undecoded json.RawMessage and never unmarshaled into memory here.
// Value-level redaction fingerprinting is the base observation path's job in
// parse.go, not this typed-depth extractor's. EndpointUris (data-plane
// connection endpoints) are intentionally not declared as a struct field at
// all, so they are never decoded into Go memory in the first place.
type spannerInstanceData struct {
	Config          string                     `json:"config"`
	DisplayName     string                     `json:"displayName"`
	NodeCount       *int64                     `json:"nodeCount"`
	ProcessingUnits *int64                     `json:"processingUnits"`
	State           string                     `json:"state"`
	Labels          map[string]json.RawMessage `json:"labels"`
}

// extractSpannerInstance extracts bounded, redaction-safe typed depth for one
// Cloud Spanner Instance CAI asset. It returns the Terraform/drift/monitoring
// attribute set (instance config short name, display name, node count or
// processing units — an instance is provisioned by exactly one of the two
// capacity modes — lifecycle state, and a bounded label count); the
// InstanceConfig full resource name as a correlation anchor; and the typed
// spanner_instance_uses_instance_config edge to that InstanceConfig (a
// separately CAI-inventoried topology resource). It emits no CMEK edge: CMEK is
// a per-database property (encryptionConfig.kmsKeyName) carried by the child
// spanner.googleapis.com/Database asset type — a separate, not-yet-registered
// extractor — not by the Instance, so fabricating a KMS edge here would assert a
// relationship the Instance resource does not carry. Raw label values, and any
// data-plane connection endpoint, are never decoded here — only a bounded label
// count crosses the redaction boundary.
func extractSpannerInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data spannerInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode spanner instance data: %w", err)
	}

	attrs := spannerInstanceAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if configName := spannerInstanceConfigFullName(data.Config, ctx.ProjectID); configName != "" {
		anchors = append(anchors, configName)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeSpannerInstanceUsesInstanceConfig,
			TargetFullResourceName: configName,
			TargetAssetType:        assetTypeSpannerInstanceConfig,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}

// spannerInstanceAttributes assembles the bounded attribute map. Blank string
// fields are omitted so a partial CAI page does not fabricate a posture. The
// *int64 capacity fields are the deliberate exception: an explicit zero is a
// real reported value (FREE_INSTANCE, or a CREATING instance) and is kept,
// while only a genuinely absent (nil) field is omitted.
func spannerInstanceAttributes(data spannerInstanceData) map[string]any {
	attrs := map[string]any{}
	if v := spannerInstanceConfigShortName(data.Config); v != "" {
		attrs["config"] = v
	}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	// Preserve an explicitly reported zero (FREE_INSTANCE, or a standard
	// instance still CREATING) and omit only a genuinely absent field. A
	// nil-check, not a > 0 guard: zero is real capacity evidence, not a
	// sparse-page artifact.
	if data.NodeCount != nil {
		attrs["node_count"] = *data.NodeCount
	}
	if data.ProcessingUnits != nil {
		attrs["processing_units"] = *data.ProcessingUnits
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if n := len(data.Labels); n > 0 {
		// int64 for type-consistency with the other numeric attributes here, so
		// the attributes map does not mix int and int64 across serializations.
		attrs["label_count"] = int64(n)
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

// spannerInstanceConfigFullName derives the CAI full resource name for the
// instance's InstanceConfig edge endpoint. The Spanner API reports `config` as
// a fully qualified "projects/<p>/instanceConfigs/<id>" relative name; that is
// prefixed with the Spanner CAI prefix directly. A bare config id (the sparse
// CAI shape, no "/") is qualified against the instance's own project, since a
// Spanner instance always references a config visible to its project (a Google-
// managed config is surfaced under each project). A blank reference yields "" so
// the caller emits no edge or anchor. An already CAI-prefixed value is returned
// as-is so a prefixed reference is never double-prefixed.
func spannerInstanceConfigFullName(config, sourceProjectID string) string {
	trimmed := strings.TrimSpace(config)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, spannerResourceNamePrefix) {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "projects/") {
		return spannerResourceNamePrefix + trimmed
	}
	project := strings.TrimSpace(sourceProjectID)
	if project == "" {
		return ""
	}
	return spannerResourceNamePrefix + "projects/" + project + "/instanceConfigs/" + trimmed
}
