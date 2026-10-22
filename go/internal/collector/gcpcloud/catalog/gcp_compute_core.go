// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Compute-core constants (lane H): instance, disk, SSL certificate, target HTTPS proxy, and URL map families (#6696).
// Compute Engine Instance constants consolidated from extractor_compute_instance.go (#6696).
// Bounded provider relationship types for the compute Engine Instance edges
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly. Only edges whose target is an exactly
// resolvable CAI resource are emitted: attached disks, and the interface network
// and subnetwork. The instance's service account is surfaced as a fingerprinted
// correlation anchor rather than an edge, because a service account email is not
// an exactly resolvable CAI resource endpoint; the secrets/IAM trust and
// image-identity layers own the "runs as" join through the same email digest.
// The asset type constants (AssetTypeComputeInstance, AssetTypeComputeDisk) and
// the compute name-derivation helpers are shared with the sibling compute
// extractors in this package.
const (
	RelationshipTypeInstanceUsesDisk     = "instance_uses_disk"
	RelationshipTypeInstanceInNetwork    = "instance_in_network"
	RelationshipTypeInstanceInSubnetwork = "instance_in_subnetwork"
)

// Compute Persistent Disk constants consolidated from extractor_disk.go (#6696).
// Asset type constants for the compute Persistent Disk typed-depth extractor and
// the relationship endpoints it derives. Each target asset type names the CAI
// asset type of an edge target so the reducer can resolve both endpoints exactly
// before materializing the edge.
//
// assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix are shared cross-group constants (see extractor/shared.go, aliased in gcpcloud/compat_extractor.go) and reused here for the disk encryption-key edge.
const (
	AssetTypeComputeDisk     = "compute.googleapis.com/Disk"
	AssetTypeComputeInstance = "compute.googleapis.com/Instance"
	AssetTypeComputeImage    = "compute.googleapis.com/Image"
	AssetTypeComputeSnapshot = "compute.googleapis.com/Snapshot"
)

// Bounded provider relationship types for the Persistent Disk edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	RelationshipTypeDiskAttachedToInstance  = "disk_attached_to_instance"
	RelationshipTypeDiskCreatedFromImage    = "disk_created_from_image"
	RelationshipTypeDiskCreatedFromSnapshot = "disk_created_from_snapshot"
	RelationshipTypeDiskEncryptedByKey      = "disk_encrypted_by_key"
)

// Compute SSL Certificate constants consolidated from extractor_ssl_certificate.go (#6696).
const (
	// AssetTypeComputeSSLCertificate is the Cloud Asset Inventory asset type for a
	// GCP Compute SSL Certificate. It is declared here in the compute-core family
	// of this leaf, and reused by the target-proxy extractors
	// (Target HTTPS Proxy, Target SSL Proxy) that resolve a `sslCertificates[]`
	// reference into this asset type as their edge target — mirroring how
	// this leaf declares AssetTypeComputeBackendService once for both proxy
	// families to reuse.
	AssetTypeComputeSSLCertificate = "compute.googleapis.com/SslCertificate"
	// SslCertificateTypeSelfManaged is the certificate type the Compute
	// sslCertificates REST schema defines as the default when the optional `type`
	// field is omitted. A self-managed certificate created without an explicit
	// type reports no `type` in resource.data, so an absent value is derived to
	// this constant rather than dropped: dropping it would degrade Terraform
	// import/drift and monitoring truth for a legitimate API shape. Deriving the
	// type reads no key material — the raw certificate/private key under
	// selfManaged is still never decoded. See
	// https://cloud.google.com/compute/docs/reference/rest/v1/sslCertificates.
	SslCertificateTypeSelfManaged = "SELF_MANAGED"
)

// Compute Target HTTPS Proxy constants consolidated from extractor_target_https_proxy.go (#6696).
// Asset type constants for the compute TargetHttpsProxy typed-depth
// extractor's edge targets. AssetTypeComputeTargetHTTPSProxy itself lives in this leaf (consolidated from extractor_forwarding_rule.go) (the ForwardingRule extractor
// resolves a `target` reference naming a targetHttpsProxies segment into that
// same asset type) and reused here; this file is the other side of that edge
// — the TargetHttpsProxy resource's own typed depth. AssetTypeComputeUrlMap
// lives in this leaf (consolidated from extractor_url_map.go) and AssetTypeComputeSSLCertificate in
// this leaf's compute-core family; both are reused here as the resolved
// edge targets rather than redeclared.
const (
	AssetTypeComputeSSLPolicy = "compute.googleapis.com/SslPolicy"
	// Certificate Manager asset types. A TargetHttpsProxy can serve TLS from
	// Certificate Manager instead of classic Compute SslCertificate resources,
	// either through the `certificateMap` reference or by listing a Certificate
	// Manager certificate self-link inside `sslCertificates`.
	AssetTypeCertificateManagerCertificateMap = "certificatemanager.googleapis.com/CertificateMap"
	AssetTypeCertificateManagerCertificate    = "certificatemanager.googleapis.com/Certificate"
)

const (
	// CertificateManagerFullNamePrefix is the CAI full-resource-name prefix for
	// Certificate Manager assets. A `certificateMap` or a Certificate Manager
	// entry in `sslCertificates` already arrives in this absolute form, or as a
	// project/location-qualified relative path this prefix is prepended to.
	CertificateManagerFullNamePrefix = "//certificatemanager.googleapis.com/"
)

// Bounded provider relationship types for TargetHttpsProxy edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	RelationshipTypeTargetHTTPSProxyUsesURLMap                 = "target_https_proxy_uses_url_map"
	RelationshipTypeTargetHTTPSProxyUsesSSLCertificate         = "target_https_proxy_uses_ssl_certificate"
	RelationshipTypeTargetHTTPSProxyUsesSSLPolicy              = "target_https_proxy_uses_ssl_policy"
	RelationshipTypeTargetHTTPSProxyUsesCertificateMap         = "target_https_proxy_uses_certificate_map"
	RelationshipTypeTargetHTTPSProxyUsesCertManagerCertificate = "target_https_proxy_uses_certificate_manager_certificate"
)

// Compute URL Map constants consolidated from extractor_url_map.go (#6696).
// Asset type constant for the compute BackendBucket relationship endpoint the
// URL Map extractor derives. AssetTypeComputeBackendService lives in this leaf (consolidated from extractor_forwarding_rule.go) and reused here; AssetTypeComputeUrlMap is declared in this leaf since URL Map is this extractor's own asset type.
const (
	AssetTypeComputeUrlMap        = "compute.googleapis.com/UrlMap"
	AssetTypeComputeBackendBucket = "compute.googleapis.com/BackendBucket"
)

// Bounded relationship types for URL Map edges. They are stable, bounded
// provider relationship strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
const (
	RelationshipTypeURLMapDefaultService     = "url_map_default_service"
	RelationshipTypeURLMapPathMatcherService = "url_map_path_matcher_default_service"
	RelationshipTypeURLMapPathRuleService    = "url_map_path_rule_service"
	// RelationshipTypeURLMapRouteRuleService covers routeRules[].service, the
	// advanced-routing analogue of pathRules[].service: exactly one direct
	// backend reference per route rule.
	RelationshipTypeURLMapRouteRuleService = "url_map_route_rule_service"
	// RelationshipTypeURLMapRouteRuleWeightedService covers each entry of
	// routeRules[].routeAction.weightedBackendServices[].backendService, a
	// distinct shape from a direct service reference because one route rule
	// can weight traffic across multiple backends (e.g. canary rollouts).
	RelationshipTypeURLMapRouteRuleWeightedService = "url_map_route_rule_weighted_service"
)
