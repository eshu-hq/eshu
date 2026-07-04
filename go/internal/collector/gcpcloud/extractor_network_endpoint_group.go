// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// assetTypeComputeNetworkEndpointGroup is declared in
// extractor_backend_service.go (the BackendService extractor already
// resolves a backend entry's `group` reference toward this asset type as its
// shared backend_service_has_backend edge target) and reused here; this file
// is the other side of that edge — the Network Endpoint Group resource's own
// typed depth. assetTypeComputeNetwork and assetTypeComputeSubnetwork are
// declared by the Subnetwork extractor and reused here.

// Bounded provider relationship types for the Network Endpoint Group edges
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly. There is no reverse edge to the
// enclosing BackendService here: that relationship is already emitted, in the
// opposite direction, by extractor_backend_service.go
// (relationshipTypeBackendServiceHasBackend), so emitting it again from this
// side would duplicate the same fact under a different relationship type.
const (
	relationshipTypeNetworkEndpointGroupInNetwork    = "network_endpoint_group_in_network"
	relationshipTypeNetworkEndpointGroupInSubnetwork = "network_endpoint_group_in_subnetwork"
)

// networkEndpointGroupServerlessCloudRun, ...AppEngine, and ...CloudFunction
// are the bounded serverless_type discriminator values surfaced for a
// SERVERLESS network endpoint group, one per mutually-exclusive serverless
// backend the Compute API supports.
const (
	networkEndpointGroupServerlessCloudRun      = "cloud_run"
	networkEndpointGroupServerlessAppEngine     = "app_engine"
	networkEndpointGroupServerlessCloudFunction = "cloud_function"
)

func init() {
	RegisterAssetExtractor(assetTypeComputeNetworkEndpointGroup, extractNetworkEndpointGroup)
}

// networkEndpointGroupData is the bounded view of a CAI
// compute.googleapis.com/NetworkEndpointGroup resource.data blob, matching
// the live Compute v1 NetworkEndpointGroup REST resource (verified against
// the Compute discovery document, 2026). Only redaction-safe control-plane
// metadata and resource references are decoded:
//
//   - networkEndpointType is decoded as a free string, not validated against a
//     hardcoded Go enum, since the Compute API is the source of truth for
//     valid values and a newly-added enum member must not fail extraction.
//   - cloudRun/appEngine/cloudFunction carry only a `service`/`function` name
//     scoped to the NEG's own project+region; they are NOT a resolvable CAI
//     resource identity (no project/region/resource-type triple), so they
//     surface as a bounded attribute pair, never an edge or anchor. Their
//     `urlMask`/`tag` fields are data-plane routing templates (the same
//     treatment as UrlMap's host/path rules) and are never decoded into a Go
//     struct field at all.
//   - pscTargetService is a bare hostname (for example
//     "asia-northeast3-cloudkms.googleapis.com"), never a resolvable CAI
//     resource; it is reduced to a deterministic host fingerprint, mirroring
//     the Pub/Sub Subscription push-endpoint host-fingerprint treatment.
//   - pscData.consumerPscAddress is a VIP IP address and is never decoded into
//     a struct field at all, per the GCP collector contract Payload
//     Boundaries (no public or private IP address is ever persisted).
//   - annotations is a label-shaped map; only its bounded count is surfaced,
//     mirroring the Filestore Instance and Workflows Workflow treatment of
//     labels/tags already captured by the collector's shared label path.
type networkEndpointGroupData struct {
	NetworkEndpointType string                                 `json:"networkEndpointType"`
	Size                json.RawMessage                        `json:"size"`
	DefaultPort         json.RawMessage                        `json:"defaultPort"`
	Zone                string                                 `json:"zone"`
	Region              string                                 `json:"region"`
	Network             string                                 `json:"network"`
	Subnetwork          string                                 `json:"subnetwork"`
	PSCTargetService    string                                 `json:"pscTargetService"`
	CreationTimestamp   string                                 `json:"creationTimestamp"`
	Annotations         map[string]string                      `json:"annotations"`
	CloudRun            *networkEndpointGroupCloudRunData      `json:"cloudRun"`
	AppEngine           *networkEndpointGroupAppEngineData     `json:"appEngine"`
	CloudFunction       *networkEndpointGroupCloudFunctionData `json:"cloudFunction"`
	PSCData             *networkEndpointGroupPSCData           `json:"pscData"`
}

// networkEndpointGroupCloudRunData is the bounded view of a NEG's `cloudRun`
// sub-object. Only `service` is decoded; `tag` and `urlMask` are data-plane
// routing values and are never declared as struct fields at all.
type networkEndpointGroupCloudRunData struct {
	Service string `json:"service"`
}

// networkEndpointGroupAppEngineData is the bounded view of a NEG's
// `appEngine` sub-object. Only `service` is decoded; `version` and `urlMask`
// are never declared as struct fields at all.
type networkEndpointGroupAppEngineData struct {
	Service string `json:"service"`
}

// networkEndpointGroupCloudFunctionData is the bounded view of a NEG's
// `cloudFunction` sub-object. Only `function` is decoded; `urlMask` is never
// declared as a struct field at all.
type networkEndpointGroupCloudFunctionData struct {
	Function string `json:"function"`
}

// networkEndpointGroupPSCData is the bounded view of a NEG's `pscData`
// sub-object, present only for a PRIVATE_SERVICE_CONNECT NEG.
// consumerPscAddress (the allocated VIP) is intentionally NOT a struct field:
// it is a private IP address and must never be decoded into Go memory, per
// the GCP collector contract Payload Boundaries.
type networkEndpointGroupPSCData struct {
	PSCConnectionID     string          `json:"pscConnectionId"`
	PSCConnectionStatus string          `json:"pscConnectionStatus"`
	ProducerPort        json.RawMessage `json:"producerPort"`
}

// extractNetworkEndpointGroup extracts bounded, redaction-safe typed depth
// for one compute NetworkEndpointGroup CAI asset. It returns the
// Terraform/drift/monitoring attribute set (endpoint type, size, default
// port, zone or region placement, creation time, serverless discriminator
// and service/function name, PSC posture and target-service fingerprint, and
// a bounded annotation count), the enclosing network/subnetwork as
// cross-source correlation anchors, and the typed network/subnetwork edges.
// No public or private IP address (a PSC consumer VIP) and no data-plane
// routing template (a serverless urlMask/tag) ever reaches the output.
func extractNetworkEndpointGroup(ctx ExtractContext) (AttributeExtraction, error) {
	var data networkEndpointGroupData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode network endpoint group data: %w", err)
	}

	attrs := networkEndpointGroupAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if networkName := computeFullResourceNameFromSelfLink(data.Network, ctx.ProjectID); networkName != "" {
		anchors = append(anchors, networkName)
		rels = append(rels, networkEndpointGroupEdge(ctx, relationshipTypeNetworkEndpointGroupInNetwork, networkName, assetTypeComputeNetwork))
	}
	if subnetName := computeFullResourceNameFromSelfLink(data.Subnetwork, ctx.ProjectID); subnetName != "" {
		anchors = append(anchors, subnetName)
		rels = append(rels, networkEndpointGroupEdge(ctx, relationshipTypeNetworkEndpointGroupInSubnetwork, subnetName, assetTypeComputeSubnetwork))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// networkEndpointGroupAttributes assembles the bounded attribute map. Empty
// or absent fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture. A NEG is either zonal
// (zone set) or regional/global (region set, or neither for a global
// internet NEG) per the Compute resource model, so both are decoded and kept
// independently rather than one being derived from the other.
func networkEndpointGroupAttributes(data networkEndpointGroupData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.NetworkEndpointType); v != "" {
		attrs["network_endpoint_type"] = v
	}
	if v, ok := parseFlexibleInt64(data.Size); ok {
		attrs["size"] = v
	}
	if v, ok := parseFlexibleInt64(data.DefaultPort); ok {
		attrs["default_port"] = v
	}
	if v := computeZoneName(data.Zone); v != "" {
		attrs["zone"] = v
	}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}

	networkEndpointGroupServerlessAttributes(data, attrs)
	networkEndpointGroupPSCAttributes(data, attrs)

	if n := len(data.Annotations); n > 0 {
		attrs["annotation_count"] = n
	}
	return attrs
}

// networkEndpointGroupServerlessAttributes sets the serverless_type
// discriminator and serverless_service name for a SERVERLESS NEG's exactly
// one configured backend (cloudRun, appEngine, or cloudFunction — the
// Compute API enforces this mutual exclusivity). No urlMask, tag, or version
// value is ever surfaced, since those are data-plane routing templates, not
// resource identities.
func networkEndpointGroupServerlessAttributes(data networkEndpointGroupData, attrs map[string]any) {
	switch {
	case data.CloudRun != nil && strings.TrimSpace(data.CloudRun.Service) != "":
		attrs["serverless_type"] = networkEndpointGroupServerlessCloudRun
		attrs["serverless_service"] = strings.TrimSpace(data.CloudRun.Service)
	case data.AppEngine != nil && strings.TrimSpace(data.AppEngine.Service) != "":
		attrs["serverless_type"] = networkEndpointGroupServerlessAppEngine
		attrs["serverless_service"] = strings.TrimSpace(data.AppEngine.Service)
	case data.CloudFunction != nil && strings.TrimSpace(data.CloudFunction.Function) != "":
		attrs["serverless_type"] = networkEndpointGroupServerlessCloudFunction
		attrs["serverless_service"] = strings.TrimSpace(data.CloudFunction.Function)
	}
}

// networkEndpointGroupPSCAttributes sets the bounded Private Service Connect
// posture: the fingerprinted target-service hostname, connection status,
// producer port, and connection id. consumerPscAddress is never read (see
// networkEndpointGroupPSCData's doc comment), so it can never be surfaced
// here even by omission-of-a-guard mistake.
func networkEndpointGroupPSCAttributes(data networkEndpointGroupData, attrs map[string]any) {
	if fp := networkEndpointGroupPSCTargetServiceFingerprint(data.PSCTargetService); fp != "" {
		attrs["psc_target_service_fingerprint"] = fp
	}
	if data.PSCData == nil {
		return
	}
	if v := strings.TrimSpace(data.PSCData.PSCConnectionStatus); v != "" {
		attrs["psc_connection_status"] = v
	}
	if v, ok := parseFlexibleInt64(data.PSCData.ProducerPort); ok {
		attrs["psc_producer_port"] = v
	}
	// pscConnectionId is a Compute-assigned uint64 that can exceed
	// float64/int64 precision; it is kept as the raw string the API reports,
	// never parsed to a numeric type.
	if v := strings.TrimSpace(data.PSCData.PSCConnectionID); v != "" {
		attrs["psc_connection_id"] = v
	}
}

// networkEndpointGroupPSCTargetServiceFingerprint returns a stable,
// case-normalized digest of a PRIVATE_SERVICE_CONNECT NEG's target-service
// hostname so NEGs sharing a target service can be correlated without
// persisting the raw DNS name, mirroring the Pub/Sub Subscription
// push-endpoint host-fingerprint treatment. A blank host fingerprints to "".
func networkEndpointGroupPSCTargetServiceFingerprint(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPNetworkEndpointGroupPSCTargetServiceHost", map[string]any{"host": normalized})
}

// networkEndpointGroupEdge builds a supported typed relationship observation
// rooted at the network endpoint group.
func networkEndpointGroupEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
