// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestIJConstantsMatchGCPCloudValues pins every data- and platform-family
// constant to the exact value it carried in package gcpcloud before the
// #6696 catalog consolidation. The consolidation moves declarations only:
// any value drift here is a behavior change, not a move.
func TestIJConstantsMatchGCPCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "AssetTypeAlloyDBCluster", got: AssetTypeAlloyDBCluster, want: "alloydb.googleapis.com/Cluster"},
		{name: "RelationshipTypeAlloyDBClusterInNetwork", got: RelationshipTypeAlloyDBClusterInNetwork, want: "alloydb_cluster_in_network"},
		{name: "RelationshipTypeAlloyDBClusterEncryptedByKMSKey", got: RelationshipTypeAlloyDBClusterEncryptedByKMSKey, want: "alloydb_cluster_encrypted_by_kms_key"},
		{name: "RelationshipTypeBigQueryDatasetKMSKey", got: RelationshipTypeBigQueryDatasetKMSKey, want: "bigquery_dataset_encrypted_by_kms_key"},
		{name: "RelationshipTypeBigQueryDatasetAuthorizesView", got: RelationshipTypeBigQueryDatasetAuthorizesView, want: "bigquery_dataset_authorizes_view"},
		{name: "RelationshipTypeBigQueryDatasetAuthorizesDataset", got: RelationshipTypeBigQueryDatasetAuthorizesDataset, want: "bigquery_dataset_authorizes_dataset"},
		{name: "RelationshipTypeBigQueryDatasetAuthorizesRoutine", got: RelationshipTypeBigQueryDatasetAuthorizesRoutine, want: "bigquery_dataset_authorizes_routine"},
		{name: "AssetTypeBigQueryRoutine", got: AssetTypeBigQueryRoutine, want: "bigquery.googleapis.com/Routine"},
		{name: "AssetTypeBigQueryConnection", got: AssetTypeBigQueryConnection, want: "bigqueryconnection.googleapis.com/Connection"},
		{name: "BigQueryConnectionResourcePrefix", got: BigQueryConnectionResourcePrefix, want: "//bigqueryconnection.googleapis.com/"},
		{name: "RelationshipTypeRoutineInDataset", got: RelationshipTypeRoutineInDataset, want: "bigquery_routine_in_dataset"},
		{name: "RelationshipTypeRoutineUsesConnection", got: RelationshipTypeRoutineUsesConnection, want: "bigquery_routine_uses_connection"},
		{name: "AssetTypeBigQueryTable", got: AssetTypeBigQueryTable, want: "bigquery.googleapis.com/Table"},
		{name: "AssetTypeBigQueryDataset", got: AssetTypeBigQueryDataset, want: "bigquery.googleapis.com/Dataset"},
		{name: "RelationshipTypeBigQueryTableInDataset", got: RelationshipTypeBigQueryTableInDataset, want: "bigquery_table_in_dataset"},
		{name: "RelationshipTypeBigQueryTableKMSKey", got: RelationshipTypeBigQueryTableKMSKey, want: "bigquery_table_encrypted_by_kms_key"},
		{name: "RelationshipTypeBigQueryTableExternalSource", got: RelationshipTypeBigQueryTableExternalSource, want: "bigquery_table_reads_external_source"},
		{name: "BigQueryResourceNamePrefix", got: BigQueryResourceNamePrefix, want: "//bigquery.googleapis.com/"},
		{name: "AssetTypeBigQueryTransferConfig", got: AssetTypeBigQueryTransferConfig, want: "bigquerydatatransfer.googleapis.com/TransferConfig"},
		{name: "RelationshipTypeTransferConfigWritesToDataset", got: RelationshipTypeTransferConfigWritesToDataset, want: "transfer_config_writes_to_dataset"},
		{name: "RelationshipTypeTransferConfigEncryptedByKMSKey", got: RelationshipTypeTransferConfigEncryptedByKMSKey, want: "transfer_config_encrypted_by_kms_key"},
		{name: "RelationshipTypeTransferConfigNotifiesTopic", got: RelationshipTypeTransferConfigNotifiesTopic, want: "transfer_config_notifies_topic"},
		{name: "AssetTypeBigtableCluster", got: AssetTypeBigtableCluster, want: "bigtableadmin.googleapis.com/Cluster"},
		{name: "BigtableClustersMarker", got: BigtableClustersMarker, want: "/clusters/"},
		{name: "RelationshipTypeBigtableClusterInInstance", got: RelationshipTypeBigtableClusterInInstance, want: "bigtable_cluster_in_instance"},
		{name: "RelationshipTypeBigtableClusterEncryptedByKMS", got: RelationshipTypeBigtableClusterEncryptedByKMS, want: "bigtable_cluster_encrypted_by_kms_key"},
		{name: "AssetTypeBigtableInstance", got: AssetTypeBigtableInstance, want: "bigtableadmin.googleapis.com/Instance"},
		{name: "AssetTypeDataflowJob", got: AssetTypeDataflowJob, want: "dataflow.googleapis.com/Job"},
		{name: "RelationshipTypeDataflowJobUsesNetwork", got: RelationshipTypeDataflowJobUsesNetwork, want: "dataflow_job_uses_network"},
		{name: "RelationshipTypeDataflowJobUsesSubnetwork", got: RelationshipTypeDataflowJobUsesSubnetwork, want: "dataflow_job_uses_subnetwork"},
		{name: "RelationshipTypeDataflowJobUsesStagingBucket", got: RelationshipTypeDataflowJobUsesStagingBucket, want: "dataflow_job_uses_staging_bucket"},
		{name: "RelationshipTypeDataflowJobEncryptedByKMSKey", got: RelationshipTypeDataflowJobEncryptedByKMSKey, want: "dataflow_job_encrypted_by_kms_key"},
		{name: "AssetTypeDataformRepository", got: AssetTypeDataformRepository, want: "dataform.googleapis.com/Repository"},
		{name: "RelationshipTypeRepositoryEncryptedByKMSKey", got: RelationshipTypeRepositoryEncryptedByKMSKey, want: "dataform_repository_encrypted_by_kms_key"},
		{name: "AssetTypeDataplexEntryGroup", got: AssetTypeDataplexEntryGroup, want: "dataplex.googleapis.com/EntryGroup"},
		{name: "AssetTypeDataprocCluster", got: AssetTypeDataprocCluster, want: "dataproc.googleapis.com/Cluster"},
		{name: "RelationshipTypeClusterUsesNetwork", got: RelationshipTypeClusterUsesNetwork, want: "dataproc_cluster_uses_network"},
		{name: "RelationshipTypeClusterUsesSubnetwork", got: RelationshipTypeClusterUsesSubnetwork, want: "dataproc_cluster_uses_subnetwork"},
		{name: "RelationshipTypeClusterEncryptedByKMSKey", got: RelationshipTypeClusterEncryptedByKMSKey, want: "dataproc_cluster_encrypted_by_kms_key"},
		{name: "RelationshipTypeClusterUsesStagingBucket", got: RelationshipTypeClusterUsesStagingBucket, want: "dataproc_cluster_uses_staging_bucket"},
		{name: "AssetTypeFilestoreInstance", got: AssetTypeFilestoreInstance, want: "file.googleapis.com/Instance"},
		{name: "RelationshipTypeFilestoreInstanceInNetwork", got: RelationshipTypeFilestoreInstanceInNetwork, want: "filestore_instance_in_network"},
		{name: "RelationshipTypeFilestoreInstanceEncryptedByKMSKey", got: RelationshipTypeFilestoreInstanceEncryptedByKMSKey, want: "filestore_instance_encrypted_by_kms_key"},
		{name: "AssetTypeFirestoreDatabase", got: AssetTypeFirestoreDatabase, want: "firestore.googleapis.com/Database"},
		{name: "RelationshipTypeFirestoreEncryptedByKMSKey", got: RelationshipTypeFirestoreEncryptedByKMSKey, want: "firestore_database_encrypted_by_kms_key"},
		{name: "AssetTypeMemcacheInstance", got: AssetTypeMemcacheInstance, want: "memcache.googleapis.com/Instance"},
		{name: "RelationshipTypeMemcacheInstanceInNetwork", got: RelationshipTypeMemcacheInstanceInNetwork, want: "memcache_instance_in_network"},
		{name: "AssetTypeRedisInstance", got: AssetTypeRedisInstance, want: "redis.googleapis.com/Instance"},
		{name: "RelationshipTypeRedisInstanceInNetwork", got: RelationshipTypeRedisInstanceInNetwork, want: "redis_instance_in_network"},
		{name: "RelationshipTypeRedisInstanceEncryptedByKMSKey", got: RelationshipTypeRedisInstanceEncryptedByKMSKey, want: "redis_instance_encrypted_by_kms_key"},
		{name: "AssetTypeSpannerDatabase", got: AssetTypeSpannerDatabase, want: "spanner.googleapis.com/Database"},
		{name: "RelationshipTypeSpannerDatabaseInInstance", got: RelationshipTypeSpannerDatabaseInInstance, want: "spanner_database_in_instance"},
		{name: "RelationshipTypeSpannerDatabaseEncryptedByKMSKey", got: RelationshipTypeSpannerDatabaseEncryptedByKMSKey, want: "spanner_database_encrypted_by_kms_key"},
		{name: "AssetTypeSpannerInstance", got: AssetTypeSpannerInstance, want: "spanner.googleapis.com/Instance"},
		{name: "AssetTypeSpannerInstanceConfig", got: AssetTypeSpannerInstanceConfig, want: "spanner.googleapis.com/InstanceConfig"},
		{name: "SpannerResourceNamePrefix", got: SpannerResourceNamePrefix, want: "//spanner.googleapis.com/"},
		{name: "RelationshipTypeSpannerInstanceUsesInstanceConfig", got: RelationshipTypeSpannerInstanceUsesInstanceConfig, want: "spanner_instance_uses_instance_config"},
		{name: "AssetTypeSQLInstance", got: AssetTypeSQLInstance, want: "sqladmin.googleapis.com/Instance"},
		{name: "SQLInstanceResourceNamePrefix", got: SQLInstanceResourceNamePrefix, want: "//sqladmin.googleapis.com/"},
		{name: "RelationshipTypeSQLInstanceInNetwork", got: RelationshipTypeSQLInstanceInNetwork, want: "sql_instance_in_network"},
		{name: "RelationshipTypeSQLInstanceEncryptedByKMSKey", got: RelationshipTypeSQLInstanceEncryptedByKMSKey, want: "sql_instance_encrypted_by_kms_key"},
		{name: "RelationshipTypeSQLInstanceHasReplica", got: RelationshipTypeSQLInstanceHasReplica, want: "sql_instance_has_replica"},
		{name: "RelationshipTypeSQLInstanceReplicaOf", got: RelationshipTypeSQLInstanceReplicaOf, want: "sql_instance_replica_of"},
		{name: "AssetTypeArtifactRegistryDockerImage", got: AssetTypeArtifactRegistryDockerImage, want: "artifactregistry.googleapis.com/DockerImage"},
		{name: "AssetTypeArtifactRegistryRepository", got: AssetTypeArtifactRegistryRepository, want: "artifactregistry.googleapis.com/Repository"},
		{name: "RelationshipTypeDockerImageInRepository", got: RelationshipTypeDockerImageInRepository, want: "docker_image_in_repository"},
		{name: "RepositoriesPathMarker", got: RepositoriesPathMarker, want: "/repositories/"},
		{name: "DockerImagesPathPrefix", got: DockerImagesPathPrefix, want: "dockerImages/"},
		{name: "DigestSeparator", got: DigestSeparator, want: "@"},
		{name: "RelationshipTypeArtifactRepoEncryptedByKMSKey", got: RelationshipTypeArtifactRepoEncryptedByKMSKey, want: "artifact_registry_repository_encrypted_by_kms_key"},
		{name: "AssetTypeCloudBuild", got: AssetTypeCloudBuild, want: "cloudbuild.googleapis.com/Build"},
		{name: "AssetTypeCloudBuildTrigger", got: AssetTypeCloudBuildTrigger, want: "cloudbuild.googleapis.com/BuildTrigger"},
		{name: "AssetTypeSourceRepo", got: AssetTypeSourceRepo, want: "sourcerepo.googleapis.com/Repository"},
		{name: "CloudBuildResourceNamePrefix", got: CloudBuildResourceNamePrefix, want: "//cloudbuild.googleapis.com/"},
		{name: "SourceRepoResourceNamePrefix", got: SourceRepoResourceNamePrefix, want: "//sourcerepo.googleapis.com/"},
		{name: "RelationshipTypeBuildTriggeredBy", got: RelationshipTypeBuildTriggeredBy, want: "build_triggered_by"},
		{name: "RelationshipTypeBuildSourceBucket", got: RelationshipTypeBuildSourceBucket, want: "build_source_bucket"},
		{name: "RelationshipTypeBuildSourceRepo", got: RelationshipTypeBuildSourceRepo, want: "build_source_repo"},
		{name: "RelationshipTypeTriggerSourceRepo", got: RelationshipTypeTriggerSourceRepo, want: "trigger_source_repo"},
		{name: "RelationshipTypeTriggerSourceRepositoryLink", got: RelationshipTypeTriggerSourceRepositoryLink, want: "trigger_source_repository_link"},
		{name: "AssetTypeDeveloperConnectGitRepositoryLink", got: AssetTypeDeveloperConnectGitRepositoryLink, want: "developerconnect.googleapis.com/GitRepositoryLink"},
		{name: "DeveloperConnectResourceNamePrefix", got: DeveloperConnectResourceNamePrefix, want: "//developerconnect.googleapis.com/"},
		{name: "DNSManagedZoneAssetType", got: DNSManagedZoneAssetType, want: "dns.googleapis.com/ManagedZone"},
		{name: "RelationshipTypeManagedZoneVisibleFromNetwork", got: RelationshipTypeManagedZoneVisibleFromNetwork, want: "dns_managed_zone_visible_from_network"},
		{name: "RelationshipTypeManagedZonePeersWithNetwork", got: RelationshipTypeManagedZonePeersWithNetwork, want: "dns_managed_zone_peers_with_network"},
		{name: "DNSPolicyAssetType", got: DNSPolicyAssetType, want: "dns.googleapis.com/Policy"},
		{name: "RelationshipTypeDNSPolicyAppliesToNetwork", got: RelationshipTypeDNSPolicyAppliesToNetwork, want: "dns_policy_applies_to_network"},
		{name: "FirebaseAppInfoAssetType", got: FirebaseAppInfoAssetType, want: "firebase.googleapis.com/FirebaseAppInfo"},
		{name: "FirebaseProjectResourceNamePrefix", got: FirebaseProjectResourceNamePrefix, want: "//firebase.googleapis.com/projects/"},
		{name: "RelationshipTypeFirebaseAppBelongsToProject", got: RelationshipTypeFirebaseAppBelongsToProject, want: "firebase_app_belongs_to_project"},
		{name: "FirebaseProjectAssetType", got: FirebaseProjectAssetType, want: "firebase.googleapis.com/FirebaseProject"},
		{name: "AssetTypeCloudResourceManagerProject", got: AssetTypeCloudResourceManagerProject, want: "cloudresourcemanager.googleapis.com/Project"},
		{name: "CloudProjectResourceNamePrefix", got: CloudProjectResourceNamePrefix, want: "//cloudresourcemanager.googleapis.com/projects/"},
		{name: "RelationshipTypeFirebaseProjectBackedByProject", got: RelationshipTypeFirebaseProjectBackedByProject, want: "firebase_project_backed_by_project"},
		{name: "RelationshipTypeFirebaseProjectDefaultBucket", got: RelationshipTypeFirebaseProjectDefaultBucket, want: "firebase_project_default_bucket"},
		{name: "FirebaseRulesetAssetType", got: FirebaseRulesetAssetType, want: "firebaserules.googleapis.com/Ruleset"},
		{name: "RelationshipTypeFirebaseRulesetBelongsToProject", got: RelationshipTypeFirebaseRulesetBelongsToProject, want: "firebase_ruleset_belongs_to_project"},
		{name: "RelationshipTypeGKEClusterUsesNetwork", got: RelationshipTypeGKEClusterUsesNetwork, want: "gke_cluster_uses_network"},
		{name: "RelationshipTypeGKEClusterUsesSubnetwork", got: RelationshipTypeGKEClusterUsesSubnetwork, want: "gke_cluster_uses_subnetwork"},
		{name: "LogBucketAssetType", got: LogBucketAssetType, want: "logging.googleapis.com/LogBucket"},
		{name: "RelationshipTypeLogBucketEncryptedByKMSKey", got: RelationshipTypeLogBucketEncryptedByKMSKey, want: "log_bucket_encrypted_by_kms_key"},
		{name: "LogSinkAssetType", got: LogSinkAssetType, want: "logging.googleapis.com/LogSink"},
		{name: "RelationshipTypeLogSinkExportsToBucket", got: RelationshipTypeLogSinkExportsToBucket, want: "log_sink_exports_to_bucket"},
		{name: "RelationshipTypeLogSinkExportsToDataset", got: RelationshipTypeLogSinkExportsToDataset, want: "log_sink_exports_to_dataset"},
		{name: "RelationshipTypeLogSinkExportsToTopic", got: RelationshipTypeLogSinkExportsToTopic, want: "log_sink_exports_to_topic"},
		{name: "RelationshipTypeLogSinkExportsToLogBucket", got: RelationshipTypeLogSinkExportsToLogBucket, want: "log_sink_exports_to_log_bucket"},
		{name: "LogSinkWriterIdentityPrefix", got: LogSinkWriterIdentityPrefix, want: "serviceAccount:"},
		{name: "AssetTypePubSubSubscription", got: AssetTypePubSubSubscription, want: "pubsub.googleapis.com/Subscription"},
		{name: "AssetTypeBigtableTable", got: AssetTypeBigtableTable, want: "bigtableadmin.googleapis.com/Table"},
		{name: "BigtableResourceNamePrefix", got: BigtableResourceNamePrefix, want: "//bigtableadmin.googleapis.com/"},
		{name: "RelationshipTypeSubscriptionSubscribesToTopic", got: RelationshipTypeSubscriptionSubscribesToTopic, want: "subscription_subscribes_to_topic"},
		{name: "RelationshipTypeSubscriptionDeadLettersToTopic", got: RelationshipTypeSubscriptionDeadLettersToTopic, want: "subscription_dead_letters_to_topic"},
		{name: "RelationshipTypeSubscriptionExportsToBigQueryTable", got: RelationshipTypeSubscriptionExportsToBigQueryTable, want: "subscription_exports_to_bigquery_table"},
		{name: "RelationshipTypeSubscriptionExportsToStorageBucket", got: RelationshipTypeSubscriptionExportsToStorageBucket, want: "subscription_exports_to_storage_bucket"},
		{name: "RelationshipTypeSubscriptionExportsToBigtableTable", got: RelationshipTypeSubscriptionExportsToBigtableTable, want: "subscription_exports_to_bigtable_table"},
		{name: "AssetTypePubSubSchema", got: AssetTypePubSubSchema, want: "pubsub.googleapis.com/Schema"},
		{name: "PubSubDeletedSchemaSentinel", got: PubSubDeletedSchemaSentinel, want: "_deleted-schema_"},
		{name: "RelationshipTypeTopicEncryptedByKMSKey", got: RelationshipTypeTopicEncryptedByKMSKey, want: "topic_encrypted_by_kms_key"},
		{name: "RelationshipTypeTopicUsesSchema", got: RelationshipTypeTopicUsesSchema, want: "topic_uses_schema"},
		{name: "RelationshipTypeStorageBucketKMSKey", got: RelationshipTypeStorageBucketKMSKey, want: "storage_bucket_encrypted_by_kms_key"},
		{name: "RelationshipTypeStorageBucketLogsToBucket", got: RelationshipTypeStorageBucketLogsToBucket, want: "storage_bucket_logs_to_bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("catalog value drift for %s: got %q want %q", tt.name, tt.got, tt.want)
			}
		})
	}

	if DeveloperConnectGitRepositoryLinkNameSegments != 8 {
		t.Fatalf("catalog value drift for DeveloperConnectGitRepositoryLinkNameSegments: got %d want 8", DeveloperConnectGitRepositoryLinkNameSegments)
	}
}
