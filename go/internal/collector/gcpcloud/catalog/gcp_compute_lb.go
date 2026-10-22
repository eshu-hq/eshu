// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Compute load-balancing constants (lane H): forwarding rule, backend service, network endpoint group, health check, and security policy families (#6696).
// Compute Forwarding Rule constants consolidated from extractor_forwarding_rule.go (#6696).
// Asset type constants for the compute ForwardingRule typed-depth extractor and
// the relationship endpoints it derives. AssetTypeComputeForwardingRule and
// AssetTypeComputeGlobalForwardingRule live in this leaf (consolidated from extractor_address.go)
// (AssetTypeComputeForwardingRule is reused there by the Address extractor for
// its used-by-forwarding-rule edge) and reused here.
// assetTypeComputeNetwork and assetTypeComputeSubnetwork are shared cross-group constants (see extractor/names.go, aliased in gcpcloud/compat_extractor.go) and reused here.
const (
	AssetTypeComputeBackendService    = "compute.googleapis.com/BackendService"
	AssetTypeComputeTargetPool        = "compute.googleapis.com/TargetPool"
	AssetTypeComputeTargetHTTPProxy   = "compute.googleapis.com/TargetHttpProxy"
	AssetTypeComputeTargetHTTPSProxy  = "compute.googleapis.com/TargetHttpsProxy"
	AssetTypeComputeTargetTCPProxy    = "compute.googleapis.com/TargetTcpProxy"
	AssetTypeComputeTargetSSLProxy    = "compute.googleapis.com/TargetSslProxy"
	AssetTypeComputeTargetGRPCProxy   = "compute.googleapis.com/TargetGrpcProxy"
	AssetTypeComputeTargetInstance    = "compute.googleapis.com/TargetInstance"
	AssetTypeComputeTargetVPNGateway  = "compute.googleapis.com/TargetVpnGateway"
	AssetTypeComputeServiceAttachment = "compute.googleapis.com/ServiceAttachment"
)

// Bounded provider relationship types for ForwardingRule edges. They are
// stable, bounded strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
// forwardingRuleTargetSegments maps the compute resource-path segment found in
// a ForwardingRule's `target` reference to its CAI asset type and bounded
// relationship type, so a new proxy kind is a single table entry rather than a
// new branch.
const (
	RelationshipTypeForwardingRuleTargetsBackendService    = "forwarding_rule_targets_backend_service"
	RelationshipTypeForwardingRuleTargetsTargetPool        = "forwarding_rule_targets_target_pool"
	RelationshipTypeForwardingRuleTargetsTargetProxy       = "forwarding_rule_targets_target_proxy"
	RelationshipTypeForwardingRuleTargetsTargetInstance    = "forwarding_rule_targets_target_instance"
	RelationshipTypeForwardingRuleTargetsTargetVPNGateway  = "forwarding_rule_targets_target_vpn_gateway"
	RelationshipTypeForwardingRuleTargetsServiceAttachment = "forwarding_rule_targets_service_attachment"
	RelationshipTypeForwardingRuleInNetwork                = "forwarding_rule_in_network"
	RelationshipTypeForwardingRuleInSubnetwork             = "forwarding_rule_in_subnetwork"
)

// Compute Backend Service constants consolidated from extractor_backend_service.go (#6696).
// Asset type constants for the compute BackendService typed-depth extractor's
// edge targets. AssetTypeComputeBackendService itself lives in this leaf (consolidated from extractor_forwarding_rule.go) (the ForwardingRule extractor resolves a
// `backendService` reference into that same asset type as its target edge)
// and reused here; this file is the other side of that edge — the
// BackendService resource's own typed depth. Cloud Asset Inventory's
// search/analysis APIs report both the regional and the global
// backend-service scope under the single `compute.googleapis.com/BackendService`
// asset type, but the list/export/monitor/query path this collector uses
// emits regional backend services under the distinct
// `compute.googleapis.com/RegionBackendService` asset type instead (see
// https://docs.cloud.google.com/asset-inventory/docs/asset-types), so both
// asset types must be registered to the same extractor function.
const (
	AssetTypeComputeRegionBackendService = "compute.googleapis.com/RegionBackendService"
	AssetTypeComputeInstanceGroup        = "compute.googleapis.com/InstanceGroup"
	AssetTypeComputeNetworkEndpointGroup = "compute.googleapis.com/NetworkEndpointGroup"
	AssetTypeComputeHealthCheck          = "compute.googleapis.com/HealthCheck"
	AssetTypeComputeSecurityPolicy       = "compute.googleapis.com/SecurityPolicy"
)

// Bounded provider relationship types for BackendService edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
// RelationshipTypeBackendServiceHasBackend is shared by both backend-group
// kinds (Instance Group and Network Endpoint Group) so a query/operator
// filters by target_asset_type for the group kind rather than by a
// proliferating set of relationship-type strings, mirroring the
// ForwardingRule extractor's shared target-proxy relationship type.
const (
	RelationshipTypeBackendServiceHasBackend             = "backend_service_has_backend"
	RelationshipTypeBackendServiceUsesHealthCheck        = "backend_service_uses_health_check"
	RelationshipTypeBackendServiceUsesSecurityPolicy     = "backend_service_uses_security_policy"
	RelationshipTypeBackendServiceUsesEdgeSecurityPolicy = "backend_service_uses_edge_security_policy"
)

// Compute Network Endpoint Group constants consolidated from extractor_network_endpoint_group.go (#6696).
// Bounded provider relationship types for the Network Endpoint Group edges
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly. There is no reverse edge to the
// enclosing BackendService here: that relationship is already emitted, in the
// opposite direction, by extractor_backend_service.go
// (RelationshipTypeBackendServiceHasBackend), so emitting it again from this
// side would duplicate the same fact under a different relationship type.
//
// RelationshipTypeNetworkEndpointGroupTargetsServerlessService is emitted only
// for a Cloud Run serverless backend with a fixed service name: the service
// resolves exactly to a run.googleapis.com/Service in the NEG's own project and
// region (mirroring the Eventarc Trigger extractor's Cloud Run edge). App
// Engine and Cloud Function serverless refs stay attribute-only, not edges,
// because neither resolves to an exact CAI endpoint from the NEG alone (the App
// Engine app id need not equal the project id; a Cloud Function ref carries no
// gen1/gen2 or region qualifier).
//
// RelationshipTypeNetworkEndpointGroupTargetsServiceAttachment is emitted only
// when a PRIVATE_SERVICE_CONNECT NEG's pscTargetService is a Producer Service
// Attachment self-link (resolving to compute.googleapis.com/ServiceAttachment,
// the same asset type the ForwardingRule extractor resolves). A bare Google API
// hostname target names no CAI resource and is host-fingerprinted instead.
const (
	RelationshipTypeNetworkEndpointGroupInNetwork                = "network_endpoint_group_in_network"
	RelationshipTypeNetworkEndpointGroupInSubnetwork             = "network_endpoint_group_in_subnetwork"
	RelationshipTypeNetworkEndpointGroupTargetsServerlessService = "network_endpoint_group_targets_serverless_service"
	RelationshipTypeNetworkEndpointGroupTargetsServiceAttachment = "network_endpoint_group_targets_service_attachment"
)

// NetworkEndpointGroupServerlessCloudRun, ...AppEngine, and ...CloudFunction
// are the bounded serverless_type discriminator values surfaced for a
// SERVERLESS network endpoint group, one per mutually-exclusive serverless
// backend the Compute API supports.
const (
	NetworkEndpointGroupServerlessCloudRun      = "cloud_run"
	NetworkEndpointGroupServerlessAppEngine     = "app_engine"
	NetworkEndpointGroupServerlessCloudFunction = "cloud_function"
)
