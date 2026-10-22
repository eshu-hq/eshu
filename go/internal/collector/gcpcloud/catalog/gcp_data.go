// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Data-family constants consolidated verbatim from the flat data-family
// extractor files (issue #6696, IJ slice). Each section names its source
// file. Values are pinned byte-identical by TestIJConstantsMatchGCPCloudValues
// in gcp_ij_test.go; any drift there is a behavior change, not a move.

// extractor_alloydb_cluster.go constants.
// Bounded provider relationship types for the AlloyDB Cluster edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	// AssetTypeAlloyDBCluster is the Cloud Asset Inventory asset type for an
	// AlloyDB Cluster. The network and CMEK edges reuse the extractor leaf shared
	// constants (extractor.AssetTypeComputeNetwork, extractor.AssetTypeKMSCryptoKey,
	// extractor.CloudKMSResourceNamePrefix).
	AssetTypeAlloyDBCluster                         = "alloydb.googleapis.com/Cluster"
	RelationshipTypeAlloyDBClusterInNetwork         = "alloydb_cluster_in_network"
	RelationshipTypeAlloyDBClusterEncryptedByKMSKey = "alloydb_cluster_encrypted_by_kms_key"
)

// extractor_bigquery_dataset.go constants.
// Bounded provider relationship types for BigQuery Dataset edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	// RelationshipTypeBigQueryDatasetKMSKey mirrors the Table KMS edge: the
	// dataset is default-encrypted by a Cloud KMS CryptoKey.
	RelationshipTypeBigQueryDatasetKMSKey = "bigquery_dataset_encrypted_by_kms_key"
	// RelationshipTypeBigQueryDatasetAuthorizesView is an authorized-view ACL
	// grant: this dataset authorizes a view (a BigQuery Table) to read it.
	RelationshipTypeBigQueryDatasetAuthorizesView = "bigquery_dataset_authorizes_view"
	// RelationshipTypeBigQueryDatasetAuthorizesDataset is an authorized-dataset
	// ACL grant between two datasets.
	RelationshipTypeBigQueryDatasetAuthorizesDataset = "bigquery_dataset_authorizes_dataset"
	// RelationshipTypeBigQueryDatasetAuthorizesRoutine is an authorized-routine
	// ACL grant to a BigQuery Routine.
	RelationshipTypeBigQueryDatasetAuthorizesRoutine = "bigquery_dataset_authorizes_routine"
	// AssetTypeBigQueryRoutine is the CAI asset type for an authorized-routine ACL
	// edge endpoint. AssetTypeBigQueryTable and AssetTypeBigQueryDataset are the
	// shared constants declared alongside it in this catalog.
	AssetTypeBigQueryRoutine = "bigquery.googleapis.com/Routine"
)

// extractor_bigquery_routine.go constants.
// AssetTypeBigQueryConnection is the CAI asset type for a BigQuery connection,
// the remote-function connection edge target. AssetTypeBigQueryRoutine,
// AssetTypeBigQueryDataset, and BigQueryResourceNamePrefix are already declared
// in this catalog.
// Bounded provider relationship types for BigQuery Routine edges. Referenced
// tables are intentionally absent: a routine's table references live inside its
// definitionBody (SQL/JS source), which is never read, so only the structured
// parent-dataset and remote-function connection references become edges.
const (
	AssetTypeBigQueryConnection           = "bigqueryconnection.googleapis.com/Connection"
	BigQueryConnectionResourcePrefix      = "//bigqueryconnection.googleapis.com/"
	RelationshipTypeRoutineInDataset      = "bigquery_routine_in_dataset"
	RelationshipTypeRoutineUsesConnection = "bigquery_routine_uses_connection"
)

// extractor_bigquery_table.go constants.
// Asset type constants for the BigQuery Table typed-depth extractor and the
// relationship endpoints it derives. Target asset types name the CAI asset type
// of each typed edge so reducers can resolve both endpoints exactly.
// Bounded relationship types for BigQuery Table edges. They are stable, bounded
// provider relationship strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
const (
	AssetTypeBigQueryTable                      = "bigquery.googleapis.com/Table"
	AssetTypeBigQueryDataset                    = "bigquery.googleapis.com/Dataset"
	RelationshipTypeBigQueryTableInDataset      = "bigquery_table_in_dataset"
	RelationshipTypeBigQueryTableKMSKey         = "bigquery_table_encrypted_by_kms_key"
	RelationshipTypeBigQueryTableExternalSource = "bigquery_table_reads_external_source"
	BigQueryResourceNamePrefix                  = "//bigquery.googleapis.com/"
)

// extractor_bigquery_transfer_config.go constants.
// Bounded provider relationship types for BigQuery Transfer Config edges. The
// data source is an enumerated source id (not a CAI resource) kept as an
// attribute, and the owner email (ownerInfo.email) is carried as a fingerprint
// anchor (an email is not an exactly resolvable CAI endpoint), so neither
// becomes an edge.
const (
	// AssetTypeBigQueryTransferConfig is the CAI asset type for a BigQuery Data
	// Transfer Service config. Its edge targets reuse asset-type constants and
	// helpers from this catalog (AssetTypeBigQueryDataset, BigQueryResourceNamePrefix)
	// and the extractor leaf (extractor.AssetTypeKMSCryptoKey,
	// extractor.CloudKMSResourceNamePrefix, extractor.AssetTypePubSubTopic with
	// extractor.PubSubTopicRefFullName).
	AssetTypeBigQueryTransferConfig                 = "bigquerydatatransfer.googleapis.com/TransferConfig"
	RelationshipTypeTransferConfigWritesToDataset   = "transfer_config_writes_to_dataset"
	RelationshipTypeTransferConfigEncryptedByKMSKey = "transfer_config_encrypted_by_kms_key"
	RelationshipTypeTransferConfigNotifiesTopic     = "transfer_config_notifies_topic"
)

// extractor_bigtable_cluster.go constants.
// Bounded provider relationship types for Bigtable Cluster edges carried on
// gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	// AssetTypeBigtableCluster is the Cloud Asset Inventory asset type for a Cloud
	// Bigtable Cluster. AssetTypeBigtableInstance is declared alongside it in this
	// catalog and reused here for the parent-instance edge;
	// extractor.AssetTypeKMSCryptoKey and extractor.CloudKMSResourceNamePrefix come
	// from the extractor leaf for the CMEK edge.
	AssetTypeBigtableCluster = "bigtableadmin.googleapis.com/Cluster"
	// BigtableClustersMarker separates a Bigtable Cluster full resource name from
	// its parent Instance full resource name. A cluster resource name is
	// ".../instances/<instance>/clusters/<cluster>", so trimming from this marker
	// yields the parent Instance identity.
	BigtableClustersMarker                        = "/clusters/"
	RelationshipTypeBigtableClusterInInstance     = "bigtable_cluster_in_instance"
	RelationshipTypeBigtableClusterEncryptedByKMS = "bigtable_cluster_encrypted_by_kms_key"
)

// extractor_bigtable_instance.go constants.
const (
	// AssetTypeBigtableInstance is the Cloud Asset Inventory asset type for a
	// Cloud Bigtable Instance.
	AssetTypeBigtableInstance = "bigtableadmin.googleapis.com/Instance"
)

// extractor_dataflow_job.go constants.
// Bounded provider relationship types for Dataflow Job edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. The job's
// runtime service account is carried as a fingerprinted-email attribute/
// anchor, not an edge, because an email is not an exactly resolvable CAI
// endpoint (mirrors the Dataproc Cluster and GKE Cluster extractors' own
// service-account treatment).
const (
	// AssetTypeDataflowJob is the Cloud Asset Inventory asset type for a Dataflow
	// Job. Its edge targets reuse asset-type constants and helpers from the extractor
	// leaf: extractor.AssetTypeComputeNetwork / extractor.AssetTypeComputeSubnetwork and
	// extractor.ComputeFullResourceNameFromSelfLink (Compute family),
	// extractor.AssetTypeStorageBucket and extractor.StorageBucketResourceNamePrefixFmt /
	// extractor.GCSBucketFromURI (Storage family), and extractor.AssetTypeKMSCryptoKey /
	// extractor.CloudKMSResourceNamePrefix for the CMEK edge.
	AssetTypeDataflowJob                         = "dataflow.googleapis.com/Job"
	RelationshipTypeDataflowJobUsesNetwork       = "dataflow_job_uses_network"
	RelationshipTypeDataflowJobUsesSubnetwork    = "dataflow_job_uses_subnetwork"
	RelationshipTypeDataflowJobUsesStagingBucket = "dataflow_job_uses_staging_bucket"
	RelationshipTypeDataflowJobEncryptedByKMSKey = "dataflow_job_encrypted_by_kms_key"
)

// extractor_dataform_repository.go constants.
const (
	// AssetTypeDataformRepository is the CAI asset type for a Dataform repository.
	// Its optional CMEK edge reuses extractor.AssetTypeKMSCryptoKey and
	// extractor.CloudKMSResourceNamePrefix from the extractor leaf.
	AssetTypeDataformRepository = "dataform.googleapis.com/Repository"
	// RelationshipTypeRepositoryEncryptedByKMSKey is the bounded provider
	// relationship type for the Dataform repository CMEK edge. It is the only typed
	// edge a repository's own resource.data can resolve: the git remote is an
	// external (non-CAI) endpoint, the service account is an email (not an exactly
	// resolvable CAI endpoint, carried as a fingerprint anchor), and workspaces and
	// compiled BigQuery datasets are child/derived resources that reference the
	// repository from their own assets rather than being named here.
	RelationshipTypeRepositoryEncryptedByKMSKey = "dataform_repository_encrypted_by_kms_key"
)

// extractor_dataplex_entry_group.go constants.
const (
	// AssetTypeDataplexEntryGroup is the CAI asset type for a Dataplex entry group.
	// An entry group is a container: its contained entries reference it from their
	// own assets (inbound), and its project is base-observation placement, so the
	// extractor derives no outbound edges or correlation anchors — only bounded
	// posture attributes.
	AssetTypeDataplexEntryGroup = "dataplex.googleapis.com/EntryGroup"
)

// extractor_dataproc_cluster.go constants.
// Bounded provider relationship types for Dataproc Cluster edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. The cluster's
// service account is carried as a fingerprinted-email anchor, not an edge,
// because an email is not an exactly resolvable CAI endpoint.
const (
	// AssetTypeDataprocCluster is the CAI asset type for a Dataproc cluster. Its edge
	// targets reuse asset-type constants and helpers from the extractor leaf:
	// extractor.AssetTypeComputeNetwork / extractor.AssetTypeComputeSubnetwork and
	// extractor.ComputeFullResourceNameFromSelfLink (Compute family),
	// extractor.AssetTypeStorageBucket and extractor.StorageBucketResourceNamePrefixFmt
	// (Storage family), and extractor.AssetTypeKMSCryptoKey /
	// extractor.CloudKMSResourceNamePrefix for the CMEK edge.
	AssetTypeDataprocCluster                 = "dataproc.googleapis.com/Cluster"
	RelationshipTypeClusterUsesNetwork       = "dataproc_cluster_uses_network"
	RelationshipTypeClusterUsesSubnetwork    = "dataproc_cluster_uses_subnetwork"
	RelationshipTypeClusterEncryptedByKMSKey = "dataproc_cluster_encrypted_by_kms_key"
	RelationshipTypeClusterUsesStagingBucket = "dataproc_cluster_uses_staging_bucket"
)

// extractor_filestore_instance.go constants.
// Bounded provider relationship types for the Filestore Instance edges carried
// on gcp_cloud_relationship facts. The reducer materializes each edge only
// when both endpoints resolve exactly. RelationshipTypeFilestoreInstanceInNetwork
// is emitted once per entry in the instance's networks array, since a
// Filestore Instance can be attached to more than one VPC network.
const (
	// AssetTypeFilestoreInstance is the Cloud Asset Inventory asset type for a
	// Filestore Instance. The authorized-network and CMEK edges reuse the extractor
	// leaf's shared constants (extractor.AssetTypeComputeNetwork,
	// extractor.AssetTypeKMSCryptoKey, extractor.CloudKMSResourceNamePrefix).
	AssetTypeFilestoreInstance                         = "file.googleapis.com/Instance"
	RelationshipTypeFilestoreInstanceInNetwork         = "filestore_instance_in_network"
	RelationshipTypeFilestoreInstanceEncryptedByKMSKey = "filestore_instance_encrypted_by_kms_key"
)

// extractor_firestore_database.go constants.
const (
	// AssetTypeFirestoreDatabase is the CAI asset type for a Firestore database. Its
	// CMEK edge target reuses extractor.AssetTypeKMSCryptoKey and
	// extractor.CloudKMSResourceNamePrefix from the extractor leaf.
	AssetTypeFirestoreDatabase = "firestore.googleapis.com/Database"
	// RelationshipTypeFirestoreEncryptedByKMSKey is the bounded provider
	// relationship type carried on the Firestore CMEK edge; the reducer materializes
	// it only when both endpoints resolve exactly.
	RelationshipTypeFirestoreEncryptedByKMSKey = "firestore_database_encrypted_by_kms_key"
)

// extractor_memcache_instance.go constants.
const (
	// AssetTypeMemcacheInstance is the Cloud Asset Inventory asset type for a
	// Memorystore for Memcached instance. The authorized-network edge reuses the
	// extractor leaf's shared network constant (extractor.AssetTypeComputeNetwork).
	AssetTypeMemcacheInstance = "memcache.googleapis.com/Instance"
	// RelationshipTypeMemcacheInstanceInNetwork is the bounded provider
	// relationship type for the Memorystore Memcached instance's authorized-
	// network edge, carried on a gcp_cloud_relationship fact. The reducer
	// materializes the edge only when both endpoints resolve exactly.
	RelationshipTypeMemcacheInstanceInNetwork = "memcache_instance_in_network"
)

// extractor_redis_instance.go constants.
// Bounded provider relationship types for the Memorystore Redis Instance edges
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly.
const (
	// AssetTypeRedisInstance is the Cloud Asset Inventory asset type for a
	// Memorystore for Redis Instance. The authorized-network and CMEK edges reuse the
	// extractor leaf's shared constants (extractor.AssetTypeComputeNetwork,
	// extractor.AssetTypeKMSCryptoKey, extractor.CloudKMSResourceNamePrefix).
	AssetTypeRedisInstance                         = "redis.googleapis.com/Instance"
	RelationshipTypeRedisInstanceInNetwork         = "redis_instance_in_network"
	RelationshipTypeRedisInstanceEncryptedByKMSKey = "redis_instance_encrypted_by_kms_key"
)

// extractor_spanner_database.go constants.
// Bounded relationship types for Spanner Database edges. They are stable
// provider relationship strings carried on gcp_cloud_relationship facts; the
// reducer materializes an edge only when both endpoints resolve exactly.
const (
	// AssetTypeSpannerDatabase is the Cloud Asset Inventory asset type for a Cloud
	// Spanner Database. It is a separately CAI-inventoried asset type from
	// spanner.googleapis.com/Instance (see AssetTypeSpannerInstance, #4317): the
	// Instance resource carries no per-database detail or CMEK, both of which
	// live on the Database resource, mirroring the BigQuery Table/Dataset
	// ownership split.
	AssetTypeSpannerDatabase                         = "spanner.googleapis.com/Database"
	RelationshipTypeSpannerDatabaseInInstance        = "spanner_database_in_instance"
	RelationshipTypeSpannerDatabaseEncryptedByKMSKey = "spanner_database_encrypted_by_kms_key"
)

// extractor_spanner_instance.go constants.
// AssetTypeSpannerInstance is the Cloud Asset Inventory asset type for a Cloud
// Spanner Instance. AssetTypeSpannerInstanceConfig is the CAI asset type for
// the instance's regional/multi-region topology config, a separately
// CAI-inventoried resource (per the Cloud Asset Inventory supported-asset-types
// list), which is why the config reference resolves to a real typed edge rather
// than staying an opaque attribute.
const (
	AssetTypeSpannerInstance       = "spanner.googleapis.com/Instance"
	AssetTypeSpannerInstanceConfig = "spanner.googleapis.com/InstanceConfig"
	// SpannerResourceNamePrefix is the CAI full-resource-name prefix for Cloud
	// Spanner resources, used to build the InstanceConfig edge endpoint from the
	// instance's `config` reference (a "projects/<p>/instanceConfigs/<id>" relative
	// name in the Spanner API).
	SpannerResourceNamePrefix = "//spanner.googleapis.com/"
	// RelationshipTypeSpannerInstanceUsesInstanceConfig is the bounded provider
	// relationship type carried on a gcp_cloud_relationship fact for the edge from
	// a Spanner Instance to the InstanceConfig that defines its regional or
	// multi-region topology. The reducer materializes the edge only when both
	// endpoints resolve exactly.
	RelationshipTypeSpannerInstanceUsesInstanceConfig = "spanner_instance_uses_instance_config"
)

// extractor_sql_instance.go constants.
// Bounded provider relationship types for the Cloud SQL Instance edges carried
// on gcp_cloud_relationship facts. The reducer materializes each edge only when
// both endpoints resolve exactly.
const (
	// AssetTypeSQLInstance is the Cloud Asset Inventory asset type for a Cloud SQL
	// Instance. The private-network and CMEK edges reuse the extractor leaf shared
	// constants (extractor.AssetTypeComputeNetwork, extractor.AssetTypeKMSCryptoKey,
	// extractor.CloudKMSResourceNamePrefix).
	AssetTypeSQLInstance = "sqladmin.googleapis.com/Instance"
	// SQLInstanceResourceNamePrefix is the CAI full-resource-name prefix for a Cloud
	// SQL Instance, used to build the master/replica edge endpoints from a bare
	// "projects/<p>/instances/<i>" reference (the sqladmin API convention for
	// masterInstanceName and replicaNames).
	SQLInstanceResourceNamePrefix                = "//sqladmin.googleapis.com/"
	RelationshipTypeSQLInstanceInNetwork         = "sql_instance_in_network"
	RelationshipTypeSQLInstanceEncryptedByKMSKey = "sql_instance_encrypted_by_kms_key"
	RelationshipTypeSQLInstanceHasReplica        = "sql_instance_has_replica"
	RelationshipTypeSQLInstanceReplicaOf         = "sql_instance_replica_of"
)
