// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Platform-family constants consolidated verbatim from the flat
// platform-family extractor files (issue #6696, IJ slice). Each section
// names its source file. Values are pinned byte-identical by
// TestIJConstantsMatchGCPCloudValues in gcp_ij_test.go; any drift there
// is a behavior change, not a move.

// extractor_artifact_registry_docker_image.go constants.
// Asset type constants for the Artifact Registry DockerImage typed-depth
// extractor and the relationship endpoint it derives. The target asset type
// names the CAI asset type of the parent-repository edge so the reducer can
// resolve both endpoints exactly before materializing.
// RepositoriesPathMarker and DockerImagesPathPrefix locate the parent repository
// inside a CAI DockerImage full resource name of the shape
// ".../repositories/<repo>/dockerImages/<image>@<digest>". The repository is the
// single segment after the repositories marker; anchoring on it (rather than the
// first "/dockerImages/" match) keeps the parse correct when the repository id is
// itself "dockerImages" or the image name embeds that token.
const (
	AssetTypeArtifactRegistryDockerImage = "artifactregistry.googleapis.com/DockerImage"
	AssetTypeArtifactRegistryRepository  = "artifactregistry.googleapis.com/Repository"
	// RelationshipTypeDockerImageInRepository is the bounded provider relationship
	// type for the image -> parent repository edge carried on a gcp_cloud_relationship
	// fact. The reducer materializes the edge only when both endpoints resolve
	// exactly. Build (built-by) and Run/GKE (deployed-to) edges are intentionally not
	// emitted here: they are cross-source correlations keyed on the content digest
	// from the deploying resource's own image references, not fields of this asset's
	// resource.data, so this extractor never fabricates an unresolved endpoint.
	RelationshipTypeDockerImageInRepository = "docker_image_in_repository"
	RepositoriesPathMarker                  = "/repositories/"
	DockerImagesPathPrefix                  = "dockerImages/"
	// DigestSeparator separates an image path from its content digest in both the
	// pullable URI and the CAI full resource name.
	DigestSeparator = "@"
)

// extractor_artifact_registry_repository.go constants.
const (
	// RelationshipTypeArtifactRepoEncryptedByKMSKey is the bounded provider
	// relationship type for the Artifact Registry repository CMEK edge. It is the
	// only typed edge a repository's own resource.data can resolve: the images and
	// packages it contains reference the repository from their own child assets
	// (see the DockerImage extractor's docker_image_in_repository edge) rather than
	// being named here, and the project is the resource's ancestry. It is a distinct
	// type from the Dataform repository CMEK edge so the two repository kinds stay
	// unambiguous in graph queries.
	RelationshipTypeArtifactRepoEncryptedByKMSKey = "artifact_registry_repository_encrypted_by_kms_key"
)

// extractor_cloud_build.go constants.
// Asset types and full-resource-name prefixes for the Cloud Build Build endpoints.
// The Storage Bucket endpoint reuses extractor.StorageBucketResourceNamePrefixFmt and the
// project/location parser is shared with the Cloud Functions extractor.
// Bounded provider relationship types for Cloud Build Build edges.
const (
	AssetTypeCloudBuild               = "cloudbuild.googleapis.com/Build"
	AssetTypeCloudBuildTrigger        = "cloudbuild.googleapis.com/BuildTrigger"
	AssetTypeSourceRepo               = "sourcerepo.googleapis.com/Repository"
	CloudBuildResourceNamePrefix      = "//cloudbuild.googleapis.com/"
	SourceRepoResourceNamePrefix      = "//sourcerepo.googleapis.com/"
	RelationshipTypeBuildTriggeredBy  = "build_triggered_by"
	RelationshipTypeBuildSourceBucket = "build_source_bucket"
	RelationshipTypeBuildSourceRepo   = "build_source_repo"
)

// extractor_cloud_build_trigger.go constants.
// Bounded provider relationship type for Cloud Build Trigger edges. The
// AssetTypeCloudBuildTrigger and AssetTypeSourceRepo asset types are declared
// in this catalog (moved from extractor_cloud_build.go) and
// are reused here, never redeclared.
// AssetTypeDeveloperConnectGitRepositoryLink is the CAI asset type for a
// Developer Connect connected repository, per the live Cloud Asset Inventory
// supported-asset-types reference. A `sourceToBuild.repository` value is
// always shaped as this asset type's resource name
// (`projects/*/locations/*/connections/*/repositories/*`) regardless of the
// underlying `repoType` (Cloud Source Repositories, GitHub, GitLab, or
// Bitbucket connected through Developer Connect) — Cloud Build's
// `GitRepoSource.repository` field never carries a `sourcerepo.googleapis.com`
// resource name, so this is a distinct target asset type from
// `AssetTypeSourceRepo` (the `triggerTemplate` legacy Cloud Source
// Repositories reference), never the same edge.
const (
	RelationshipTypeTriggerSourceRepo             = "trigger_source_repo"
	RelationshipTypeTriggerSourceRepositoryLink   = "trigger_source_repository_link"
	AssetTypeDeveloperConnectGitRepositoryLink    = "developerconnect.googleapis.com/GitRepositoryLink"
	DeveloperConnectResourceNamePrefix            = "//developerconnect.googleapis.com/"
	DeveloperConnectGitRepositoryLinkNameSegments = 8 // projects/<p>/locations/<l>/connections/<c>/repositories/<r>
)

// extractor_dns_managed_zone.go constants.
// Bounded provider relationship types for the Managed Zone's network edges.
// They are stable provider relationship strings carried on
// gcp_cloud_relationship facts; the reducer materializes an edge only when both
// endpoints resolve exactly.
const (
	// DNSManagedZoneAssetType is the Cloud Asset Inventory asset type for a Cloud
	// DNS Managed Zone. It is distinct from assetTypeDNSResourceRecordSet, which
	// covers the zone's own record sets and flows through the separate
	// gcp_dns_record fact family (parseDNSRecords), not this typed-depth extractor.
	DNSManagedZoneAssetType = "dns.googleapis.com/ManagedZone"
	// RelationshipTypeManagedZoneVisibleFromNetwork marks a private zone's bind
	// to a VPC network that can resolve it (privateVisibilityConfig.networks).
	RelationshipTypeManagedZoneVisibleFromNetwork = "dns_managed_zone_visible_from_network"
	// RelationshipTypeManagedZonePeersWithNetwork marks a DNS peering zone's
	// forwarding bind to the peer VPC network (peeringConfig.targetNetwork).
	RelationshipTypeManagedZonePeersWithNetwork = "dns_managed_zone_peers_with_network"
)

// extractor_dns_policy.go constants.
const (
	// DNSPolicyAssetType is the Cloud Asset Inventory asset type for a Cloud DNS
	// Policy. It is distinct from DNSManagedZoneAssetType: a Policy binds
	// inbound-forwarding, logging, and alternative-name-server behavior to a set
	// of VPC networks, while a ManagedZone is a DNS namespace with its own
	// visibility and peering configuration.
	DNSPolicyAssetType = "dns.googleapis.com/Policy"
	// RelationshipTypeDNSPolicyAppliesToNetwork marks a DNS Policy's bind to a VPC
	// network it governs (networks[].networkUrl). It is a stable provider
	// relationship string carried on gcp_cloud_relationship facts; the reducer
	// materializes an edge only when both endpoints resolve exactly.
	RelationshipTypeDNSPolicyAppliesToNetwork = "dns_policy_applies_to_network"
)

// extractor_firebase_app_info.go constants.
const (
	// FirebaseAppInfoAssetType is the Cloud Asset Inventory asset type for a Firebase
	// App Info summary (a Web, Android, or iOS app registered under a Firebase
	// project). The parent project edge targets FirebaseProjectAssetType in this catalog.
	FirebaseAppInfoAssetType = "firebase.googleapis.com/FirebaseAppInfo"
	// FirebaseProjectResourceNamePrefix is the canonical CAI full-resource-name
	// prefix for a Firebase Project (`//firebase.googleapis.com/projects/<projectId>`).
	FirebaseProjectResourceNamePrefix = "//firebase.googleapis.com/projects/"
	// RelationshipTypeFirebaseAppBelongsToProject is the bounded provider
	// relationship type for the edge from a Firebase app to its parent Firebase
	// project.
	RelationshipTypeFirebaseAppBelongsToProject = "firebase_app_belongs_to_project"
)

// extractor_firebase_project.go constants.
// AssetTypeCloudResourceManagerProject and CloudProjectResourceNamePrefix key the
// backing GCP project. Cloud Asset Inventory names a Project by its project
// number (`//cloudresourcemanager.googleapis.com/projects/<number>`), so the
// backing edge is only emitted when the FirebaseProject carries projectNumber;
// deriving it from the project id would target an unresolvable name.
// Bounded provider relationship types for the Firebase project's backing GCP
// project and its default Cloud Storage bucket.
const (
	// FirebaseProjectAssetType is the Cloud Asset Inventory asset type for a Firebase
	// Project. The default Storage bucket endpoint reuses extractor.AssetTypeStorageBucket
	// and extractor.StorageBucketResourceNamePrefixFmt from the extractor leaf.
	FirebaseProjectAssetType                       = "firebase.googleapis.com/FirebaseProject"
	AssetTypeCloudResourceManagerProject           = "cloudresourcemanager.googleapis.com/Project"
	CloudProjectResourceNamePrefix                 = "//cloudresourcemanager.googleapis.com/projects/"
	RelationshipTypeFirebaseProjectBackedByProject = "firebase_project_backed_by_project"
	RelationshipTypeFirebaseProjectDefaultBucket   = "firebase_project_default_bucket"
)

// extractor_firebase_ruleset.go constants.
const (
	// FirebaseRulesetAssetType is the Cloud Asset Inventory asset type for a Firebase
	// Rules Ruleset. The parent project edge targets FirebaseProjectAssetType and
	// reuses FirebaseProjectResourceNamePrefix from this catalog.
	FirebaseRulesetAssetType = "firebaserules.googleapis.com/Ruleset"
	// RelationshipTypeFirebaseRulesetBelongsToProject is the bounded provider
	// relationship type for the edge from a ruleset to its parent Firebase project.
	RelationshipTypeFirebaseRulesetBelongsToProject = "firebase_ruleset_belongs_to_project"
)

// extractor_gke_cluster.go constants.
// Bounded provider relationship types for GKE Cluster edges. Each is a stable
// string carried on a gcp_cloud_relationship fact; the reducer materializes an
// edge only when both endpoints resolve exactly. A node pool's own service
// account is carried per-pool as a fingerprinted-email attribute/anchor, not an
// edge, because an email is not an exactly resolvable CAI endpoint (mirrors the
// Dataproc Cluster extractor's cluster-service-account treatment).
const (
	RelationshipTypeGKEClusterUsesNetwork    = "gke_cluster_uses_network"
	RelationshipTypeGKEClusterUsesSubnetwork = "gke_cluster_uses_subnetwork"
)

// extractor_log_bucket.go constants.
const (
	// LogBucketAssetType is the Cloud Asset Inventory asset type for a GCP Logging
	// Log Bucket. The CMEK CryptoKey endpoint reuses extractor.AssetTypeKMSCryptoKey and
	// extractor.CloudKMSResourceNamePrefix from the extractor leaf.
	LogBucketAssetType = "logging.googleapis.com/LogBucket"
	// RelationshipTypeLogBucketEncryptedByKMSKey is the bounded provider relationship
	// type for the edge from a log bucket to its CMEK CryptoKey.
	RelationshipTypeLogBucketEncryptedByKMSKey = "log_bucket_encrypted_by_kms_key"
)

// extractor_log_sink.go constants.
// Bounded provider relationship types for the sink's export destination.
const (
	// LogSinkAssetType is the Cloud Asset Inventory asset type for a GCP Logging Log
	// Sink. Destination endpoint asset types (Storage Bucket, BigQuery Dataset,
	// Pub/Sub Topic, Log Bucket) reuse extractor leaf and catalog constants
	// (extractor.AssetTypeStorageBucket, AssetTypeBigQueryDataset,
	// extractor.AssetTypePubSubTopic, LogBucketAssetType).
	LogSinkAssetType                          = "logging.googleapis.com/LogSink"
	RelationshipTypeLogSinkExportsToBucket    = "log_sink_exports_to_bucket"
	RelationshipTypeLogSinkExportsToDataset   = "log_sink_exports_to_dataset"
	RelationshipTypeLogSinkExportsToTopic     = "log_sink_exports_to_topic"
	RelationshipTypeLogSinkExportsToLogBucket = "log_sink_exports_to_log_bucket"
	LogSinkWriterIdentityPrefix               = "serviceAccount:"
)

// extractor_pubsub_subscription.go constants.
// Bounded provider relationship types for Pub/Sub Subscription edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	// AssetTypePubSubSubscription is the CAI asset type for a Pub/Sub Subscription.
	// Its edge targets reuse asset-type constants from this catalog and the extractor
	// leaf: extractor.AssetTypePubSubTopic, AssetTypeBigQueryTable,
	// extractor.AssetTypeStorageBucket, and their //-resource-name prefixes.
	AssetTypePubSubSubscription = "pubsub.googleapis.com/Subscription"
	// AssetTypeBigtableTable is the CAI asset type for a Cloud Bigtable table, the
	// target of a Bigtable-export subscription's sink edge.
	AssetTypeBigtableTable = "bigtableadmin.googleapis.com/Table"
	// BigtableResourceNamePrefix is the CAI full-resource-name prefix for Bigtable
	// resources.
	BigtableResourceNamePrefix                         = "//bigtableadmin.googleapis.com/"
	RelationshipTypeSubscriptionSubscribesToTopic      = "subscription_subscribes_to_topic"
	RelationshipTypeSubscriptionDeadLettersToTopic     = "subscription_dead_letters_to_topic"
	RelationshipTypeSubscriptionExportsToBigQueryTable = "subscription_exports_to_bigquery_table"
	RelationshipTypeSubscriptionExportsToStorageBucket = "subscription_exports_to_storage_bucket"
	RelationshipTypeSubscriptionExportsToBigtableTable = "subscription_exports_to_bigtable_table"
)

// extractor_pubsub_topic.go constants.
// Bounded provider relationship types for Pub/Sub Topic edges. Each is a stable,
// bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. Subscription
// and IAM publisher/subscriber edges are intentionally absent: a Topic's own
// resource.data carries neither its subscriptions (which reference the topic on
// their own asset) nor its IAM policy (never persisted per the collector
// contract), so those joins belong to the subscription asset and IAM-policy
// paths, not to this metadata-only extractor.
const (
	// AssetTypePubSubSchema is the CAI asset type for a Pub/Sub Schema, the target
	// of a topic's schema edge. The Topic asset type (extractor.AssetTypePubSubTopic)
	// and the //pubsub.googleapis.com/ full-resource-name prefix
	// (extractor.PubSubResourceNamePrefix) live in the extractor leaf; the KMS
	// CryptoKey asset type and prefix come from there too.
	AssetTypePubSubSchema = "pubsub.googleapis.com/Schema"
	// PubSubDeletedSchemaSentinel is the value Pub/Sub reports for schemaSettings.schema
	// when the referenced schema has been deleted while still attached to the topic.
	// It names no resolvable Schema resource, so it yields no edge or anchor.
	PubSubDeletedSchemaSentinel            = "_deleted-schema_"
	RelationshipTypeTopicEncryptedByKMSKey = "topic_encrypted_by_kms_key"
	RelationshipTypeTopicUsesSchema        = "topic_uses_schema"
)

// extractor_storage_bucket.go constants.
// Bounded provider relationship types for Cloud Storage Bucket edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	// RelationshipTypeStorageBucketKMSKey is the CMEK default-encryption edge from
	// the bucket to its Cloud KMS CryptoKey.
	RelationshipTypeStorageBucketKMSKey = "storage_bucket_encrypted_by_kms_key"
	// RelationshipTypeStorageBucketLogsToBucket is the usage-logging export edge
	// from the bucket to the destination log bucket.
	RelationshipTypeStorageBucketLogsToBucket = "storage_bucket_logs_to_bucket"
)
