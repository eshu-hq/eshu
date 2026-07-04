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
//
// relationshipTypeNetworkEndpointGroupTargetsServerlessService is emitted only
// for a Cloud Run serverless backend with a fixed service name: the service
// resolves exactly to a run.googleapis.com/Service in the NEG's own project and
// region (mirroring the Eventarc Trigger extractor's Cloud Run edge). App
// Engine and Cloud Function serverless refs stay attribute-only, not edges,
// because neither resolves to an exact CAI endpoint from the NEG alone (the App
// Engine app id need not equal the project id; a Cloud Function ref carries no
// gen1/gen2 or region qualifier).
//
// relationshipTypeNetworkEndpointGroupTargetsServiceAttachment is emitted only
// when a PRIVATE_SERVICE_CONNECT NEG's pscTargetService is a Producer Service
// Attachment self-link (resolving to compute.googleapis.com/ServiceAttachment,
// the same asset type the ForwardingRule extractor resolves). A bare Google API
// hostname target names no CAI resource and is host-fingerprinted instead.
const (
	relationshipTypeNetworkEndpointGroupInNetwork                = "network_endpoint_group_in_network"
	relationshipTypeNetworkEndpointGroupInSubnetwork             = "network_endpoint_group_in_subnetwork"
	relationshipTypeNetworkEndpointGroupTargetsServerlessService = "network_endpoint_group_targets_serverless_service"
	relationshipTypeNetworkEndpointGroupTargetsServiceAttachment = "network_endpoint_group_targets_service_attachment"
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
//   - cloudRun carries a `service` name scoped to the NEG's own project and
//     region, which resolves exactly to a run.googleapis.com/Service, so it
//     yields a typed edge (mirroring the Eventarc Trigger extractor). appEngine
//     and cloudFunction carry only a `service`/`function` name that does not
//     resolve to an exact CAI endpoint from the NEG alone, so they surface as a
//     bounded attribute only. Their `urlMask`/`tag`/`version` fields are
//     data-plane routing templates (the same treatment as UrlMap's host/path
//     rules) and are never decoded into a Go struct field at all.
//   - pscTargetService is either a Producer Service Attachment self-link, which
//     resolves to a compute.googleapis.com/ServiceAttachment edge, or a bare
//     Google API hostname (for example "asia-northeast3-cloudkms.googleapis.com"),
//     which names no CAI resource and is reduced to a deterministic host
//     fingerprint, mirroring the Pub/Sub Subscription push-endpoint treatment.
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
// and service/function name, PSC posture, and a bounded annotation count),
// cross-source correlation anchors, and the typed edges: the enclosing
// network and subnetwork, the targeted Cloud Run service for a Cloud Run
// serverless NEG, and the targeted Service Attachment for a PSC NEG whose
// pscTargetService is a Producer Service Attachment self-link. No public or
// private IP address (a PSC consumer VIP) and no data-plane routing template
// (a serverless urlMask/tag) ever reaches the output.
func extractNetworkEndpointGroup(ctx ExtractContext) (AttributeExtraction, error) {
	var data networkEndpointGroupData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode network endpoint group data: %w", err)
	}

	attrs := map[string]any{}
	networkEndpointGroupCoreAttributes(data, attrs)

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

	serverlessRels, serverlessAnchors := networkEndpointGroupServerlessAttributes(ctx, data, attrs)
	rels = append(rels, serverlessRels...)
	anchors = append(anchors, serverlessAnchors...)

	pscRels, pscAnchors := networkEndpointGroupPSCAttributes(ctx, data, attrs)
	rels = append(rels, pscRels...)
	anchors = append(anchors, pscAnchors...)

	if n := len(data.Annotations); n > 0 {
		attrs["annotation_count"] = n
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// networkEndpointGroupCoreAttributes fills the bounded scalar attribute set.
// Empty or absent fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture. A NEG is either zonal
// (zone set) or regional/global (region set, or neither for a global
// internet NEG) per the Compute resource model, so both are decoded and kept
// independently rather than one being derived from the other.
func networkEndpointGroupCoreAttributes(data networkEndpointGroupData, attrs map[string]any) {
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
}

// networkEndpointGroupServerlessAttributes sets the serverless_type
// discriminator for a SERVERLESS NEG's exactly one configured backend
// (cloudRun, appEngine, or cloudFunction — the Compute API enforces this
// mutual exclusivity). The discriminator is set from sub-object PRESENCE, not
// from a non-empty name, because a URL-mask NEG carries the sub-object with no
// fixed service/function name (the name is parsed from the request URL at
// runtime) yet is still a valid Cloud Run / App Engine / Cloud Function NEG
// that inventory must be able to distinguish. serverless_service is added only
// when a fixed name is present, and the Cloud Run service — the one serverless
// backend that resolves exactly to a CAI resource in the NEG's own project and
// region — emits a typed edge, mirroring the Eventarc Trigger extractor. No
// urlMask, tag, or version value is ever surfaced, since those are data-plane
// routing templates, not resource identities.
func networkEndpointGroupServerlessAttributes(ctx ExtractContext, data networkEndpointGroupData, attrs map[string]any) ([]RelationshipObservation, []string) {
	switch {
	case data.CloudRun != nil:
		attrs["serverless_type"] = networkEndpointGroupServerlessCloudRun
		service := strings.TrimSpace(data.CloudRun.Service)
		if service == "" {
			return nil, nil
		}
		attrs["serverless_service"] = service
		serviceName := networkEndpointGroupRunServiceFullName(ctx.ProjectID, data.Region, service)
		if serviceName == "" {
			return nil, nil
		}
		return []RelationshipObservation{
			networkEndpointGroupEdge(ctx, relationshipTypeNetworkEndpointGroupTargetsServerlessService, serviceName, assetTypeRunService),
		}, []string{serviceName}
	case data.AppEngine != nil:
		attrs["serverless_type"] = networkEndpointGroupServerlessAppEngine
		if service := strings.TrimSpace(data.AppEngine.Service); service != "" {
			attrs["serverless_service"] = service
		}
	case data.CloudFunction != nil:
		attrs["serverless_type"] = networkEndpointGroupServerlessCloudFunction
		if fn := strings.TrimSpace(data.CloudFunction.Function); fn != "" {
			attrs["serverless_service"] = fn
		}
	}
	return nil, nil
}

// networkEndpointGroupPSCAttributes sets the bounded Private Service Connect
// posture: connection status, producer port, and connection id from pscData,
// plus target-service resolution. A pscTargetService that is a Producer
// Service Attachment self-link resolves to a compute.googleapis.com/
// ServiceAttachment and emits a typed edge (the same asset type the
// ForwardingRule extractor resolves); a bare Google API hostname names no CAI
// resource and is reduced to a deterministic host fingerprint instead — the
// two cases are mutually exclusive, so exactly one of the edge or the
// fingerprint is produced. consumerPscAddress is never read (see
// networkEndpointGroupPSCData's doc comment), so it can never be surfaced here
// even by omission-of-a-guard mistake.
func networkEndpointGroupPSCAttributes(ctx ExtractContext, data networkEndpointGroupData, attrs map[string]any) ([]RelationshipObservation, []string) {
	var rels []RelationshipObservation
	var anchors []string
	if saName := computeResourceFullNameFromSelfLink(data.PSCTargetService, "serviceAttachments", ctx.ProjectID); saName != "" {
		attrs["psc_target_service_attachment"] = saName
		anchors = append(anchors, saName)
		rels = append(rels, networkEndpointGroupEdge(ctx, relationshipTypeNetworkEndpointGroupTargetsServiceAttachment, saName, assetTypeComputeServiceAttachment))
	} else if fp := networkEndpointGroupPSCTargetServiceFingerprint(data.PSCTargetService); fp != "" {
		attrs["psc_target_service_fingerprint"] = fp
	}
	if data.PSCData != nil {
		if v := strings.TrimSpace(data.PSCData.PSCConnectionStatus); v != "" {
			attrs["psc_connection_status"] = v
		}
		if v, ok := parseFlexibleInt64(data.PSCData.ProducerPort); ok {
			attrs["psc_producer_port"] = v
		}
		// pscConnectionId is a Compute-assigned uint64 that can exceed
		// float64/int64 precision; it is kept as the raw string the API
		// reports, never parsed to a numeric type.
		if v := strings.TrimSpace(data.PSCData.PSCConnectionID); v != "" {
			attrs["psc_connection_id"] = v
		}
	}
	return rels, anchors
}

// networkEndpointGroupRunServiceFullName builds the CAI Cloud Run Service full
// resource name for a serverless NEG's cloudRun.service. The service is a
// control-plane resource in the NEG's own project (ctx.ProjectID) and region
// (the NEG's own region self-link), so it resolves exactly. It returns "" when
// the project, region, or service is missing, so no unresolved edge is minted.
func networkEndpointGroupRunServiceFullName(projectID, regionRef, service string) string {
	project := strings.TrimSpace(projectID)
	region := computeRegionName(regionRef)
	svc := strings.TrimSpace(service)
	if project == "" || region == "" || svc == "" {
		return ""
	}
	return runServiceResourceNamePrefix + "projects/" + project + "/locations/" + region + "/services/" + svc
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
