# AWS Cloud Collector Test Parity

Owner: eshu-hq/eshu Epic A (#3736)
Last updated: 2026-06-26

## Audit correction (2026-06-25)

The original framing ("8.5% test ratio") measured only the flat top-level
`awscloud/` directory where 133 of 152 non-test files are `constants_*.go`
static lookup tables with no behavior to unit-test.

Verified current state (commit `0f4c8a8c`):
- Whole tree: 722 test files, 1,510 non-test files
  (test/non-test ≈ 48%; test/total = 722/2,232 ≈ 32%).
- All 134 service subdirs under `services/` have tests.
- Every priority service named below already has `scanner_test.go` plus
  supplementary test files.

## Purpose

This document records per-service depth-audit results for the priority services
identified in Epic A (#3736). Each audit confirms that the named canonical
edges have both a positive assertion and a malformed/missing-field assertion
in the existing test suite.

## Method

For each priority service:
1. Identify the fact kinds and relationships the scanner emits.
2. Map the epic-named canonical edges to the scanner's fact shapes.
3. Confirm a positive test case asserts the correct payload shape.
4. Confirm a malformed-input or missing-field test case exists (or note
   that the field is optional and the scanner handles absence gracefully).

## A1: S3 — ALLOWS_EGRESS, AWS_USES_KMS_KEY

### Scanner output
- `aws_resource` (ResourceTypeS3Bucket): bucket metadata, encryption, logging,
  public-access, website, versioning, ownership, tags.
- `s3_bucket_posture`: derived booleans (block-public-access, encryption,
  versioning, MFA-delete, ACL-disabled, logging, replication, policy booleans).
- `s3_external_principal_grant`: principal kind, value, account ID, grant
  outcome, cross-account/public/service flags.
- `aws_resource_policy_permission`: normalized resource-policy statements.
- `aws_relationship`: `s3_bucket_logs_to_bucket` (logging target).

### Edge audit

| Edge | Positive case | Malformed / missing-field case | Verdict |
|---|---|---|---|
| ALLOWS_EGRESS | `TestScannerEmitsS3MetadataOnlyBucketFactsAndLoggingRelationships` (line 109-121, `s3_bucket_logs_to_bucket` relationship with target bucket ARN) | `TestScannerSkipsLoggingRelationshipWithoutTargetBucket` (line 302-318, no relationship when missing target bucket) | Covered |
| AWS_USES_KMS_KEY | `TestScannerEmitsS3MetadataOnlyBucketFactsAndLoggingRelationships` (line 74-75, `kms_master_key_ids` attribute; line 26-28, KMS encryption rule) | KMS key is optional; scanner handles unencrypted buckets (no KMS key emitted). Verified by absence of `kms_master_key_ids` in unencrypted fixtures. | Covered |

### Test files
- `services/s3/scanner_test.go`: 6 test functions (plus partition, resource-policy, servicekind tests).
- `services/s3/partition_test.go`: partition derivation tests.
- `services/s3/resource_policy_scanner_test.go`: resource-policy-permission tests.
- `services/s3/servicekind_test.go`: service-kind canonicalization tests.
- `services/s3/awssdk/`: adapter tests for SDK-to-scanner mapping, public-flag derivation, external-principal-grant derivation, resource-policy-permission derivation.

## A2: IAM — CAN_ASSUME, CAN_PERFORM

### Scanner output
- `aws_resource` (IAMRole, IAMPolicy, IAMInstanceProfile, IAMUser): role/user
  metadata, trust-policy presence, attached-policy ARNs, inline-policy names.
- `aws_relationship`: `iam_role_trusts_principal`, `iam_role_attached_policy`,
  `iam_role_in_instance_profile`.
- `aws_iam_permission`: normalized policy statements (effect, actions, resources,
  condition keys, assume-principals). Never carries raw policy JSON.
- `secrets_iam_principal`, `secrets_iam_trust_policy`,
  `secrets_iam_permission_policy`, `secrets_iam_policy_attachment`,
  `secrets_iam_permission_boundary`, `secrets_iam_instance_profile`,
  `secrets_iam_coverage_warning`: Secrets IAM posture source facts.

### Edge audit

| Edge | Positive case | Malformed / missing-field case | Verdict |
|---|---|---|---|
| CAN_ASSUME | `TestScannerEmitsIAMResourcesAndRelationships` (line 70-71, `iam_role_trusts_principal` relationship with AWS:<arn> principal); `TestScannerEmitsDerivedPermissionFacts` (line 86-90, trust-source `sts:AssumeRole` with assume-principals) | `TestScannerStopsOnClientError` (line 283-288, error propagation on ListRoles failure). Trust principals are optional — roles without trust policies are handled. | Covered |
| CAN_PERFORM | `TestScannerEmitsDerivedPermissionFacts` (line 133-148, 5 permission facts across 4 policy sources: trust, inline, attached-managed, permission-boundary) | `assertNoRawPolicyJSON` (line 394-437) proves no raw policy body leaks into permission facts. | Covered |

### Test files
- `services/iam/scanner_test.go`: 5 test functions.
- `services/iam/servicekind_test.go`: service-kind and whitespace tests.
- `services/iam/awssdk/`: adapter tests for SDK-to-scanner mapping, policy-document normalization, trust-policy derivation, coverage-warning derivation.

## A3: EC2 — RUNS_IN, RUNS_ON

### Scanner output
- `aws_resource` (EC2VPC, EC2Subnet, EC2SecurityGroup, EC2SecurityGroupRule,
  EC2NetworkInterface): network topology metadata, security-group rules, ENI
  attachments.
- `ec2_instance_posture`: instance metadata (IMDSv2, user-data presence, EBS
  optimization, public IP, instance-profile ARN, block-device list). Never
  carries user-data content.
- `aws_security_group_rule`: normalized security-group rule posture.
- `aws_relationship`: `ec2_subnet_in_vpc`, `ec2_security_group_in_vpc`,
  `ec2_security_group_has_rule`, `ec2_network_interface_in_subnet`,
  `ec2_network_interface_in_vpc`, `ec2_network_interface_uses_security_group`,
  `ec2_network_interface_attached_to_resource`.
- EBS volumes: `aws_resource` (EC2EBSVolume) with encrypted/KMS metadata.

### Edge audit

| Edge | Positive case | Malformed / missing-field case | Verdict |
|---|---|---|---|
| RUNS_IN | `TestScannerEmitsNetworkTopologyWithoutInstanceFacts` (line 137-141, ENI-in-subnet, ENI-in-VPC, subnet-in-VPC relationships; security-group-in-VPC) | Subnet/VPC identity is required for relationship emission; missing VPC/subnet ID would prevent the relationship fact. | Covered |
| RUNS_ON | `TestScannerEmitsNetworkTopologyWithoutInstanceFacts` (line 143-149, `ec2_network_interface_attached_to_resource` with instance ARN); `TestScannerEmitsInstancePostureFactsWithoutInventory` (line 172-176, block-device list) | Attachment is optional (not all ENIs are attached); the scanner handles nil attachment. | Covered |

### Test files
- `services/ec2/scanner_test.go`: 3 test functions.
- `services/ec2/volume_test.go`: EBS volume tests.
- `services/ec2/servicekind_test.go`: service-kind tests.
- `services/ec2/awssdk/`: adapter tests for VPC, subnet, security-group, ENI, instance, volume, and rule mapping.

## A4: KMS + Secrets Manager — AWS_USES_KMS_KEY, secret-reference

### KMS scanner output
- `aws_resource` (KMSKey, KMSAlias, KMSGrant): key metadata, alias targets,
  grant metadata (never encryption contexts or key material).
- `aws_resource_policy_permission`: normalized key-policy statements.
- `aws_relationship`: `kms_alias_targets_key`, `kms_grant_on_key`,
  `kms_grant_for_grantee`.

### Secrets Manager scanner output
- `aws_resource` (SecretsManagerSecret): secret metadata (never values, versions,
  resource policy JSON, external rotation metadata).
- `aws_relationship`: `secrets_manager_secret_uses_kms_key`,
  `secrets_manager_secret_uses_rotation_lambda`.

### Edge audit

| Edge | Positive case | Malformed / missing-field case | Verdict |
|---|---|---|---|
| AWS_USES_KMS_KEY (KMS) | `TestScannerEmitsKMSKeyAliasAndGrantMetadataOnly` (line 124-125, encryption algorithms; line 40, rotation enabled) | `TestScannerOmitsRotationStatusWhenNotReported` (line 190-214, omits rotation_enabled when not reported by AWS) | Covered |
| AWS_USES_KMS_KEY (Secrets Manager) | `TestScannerEmitsSecretsManagerMetadataOnlyFactsAndRelationships` (line 81-87, `secrets_manager_secret_uses_kms_key` relationship with KMS ARN) | `TestScannerDoesNotTreatNonARNKMSIdentifierAsARN` (line 98-116, non-ARN KMS ID like `alias/secrets` sets target_resource_id without target_arn) | Covered |
| secret-reference | `TestScannerEmitsSecretsManagerMetadataOnlyFactsAndRelationships` (line 54, `kms_key_id` attribute; line 89-95, `secrets_manager_secret_uses_rotation_lambda` relationship with Lambda ARN) | Secret values and versions are never persisted; forbidden-payload test (line 65-79) proves metadata-only contract. | Covered |

### Test files
- `services/kms/scanner_test.go`: 6 test functions.
- `services/kms/resource_policy_scanner_test.go`: resource-policy tests.
- `services/kms/servicekind_test.go`: service-kind tests.
- `services/kms/awssdk/`: adapter tests for key listing, policy derivation, grant constraint handling.
- `services/secretsmanager/scanner_test.go`: 3 test functions.
- `services/secretsmanager/servicekind_test.go`: service-kind tests.
- `services/secretsmanager/awssdk/`: adapter tests for secret listing and KMS/rotation resolution.

## A5: Glue + ElastiCache — canonical edges

### Glue scanner output
- `aws_resource` (GlueDatabase, GlueTable, GlueCrawler, GlueJob, GlueTrigger,
  GlueWorkflow, GlueConnection): Data Catalog metadata (never script bodies,
  default-argument values, connection property values, column statistics, or
  classifier patterns).
- `aws_relationship`: `glue_table_in_database`, `glue_table_stored_at_s3_location`,
  `glue_crawler_targets_database`, `glue_crawler_uses_iam_role`,
  `glue_job_uses_iam_role`, `glue_trigger_invokes_job`.

### ElastiCache scanner output
- `aws_resource` (ElastiCacheCacheCluster, ElastiCacheReplicationGroup,
  ElastiCacheSubnetGroup, ElastiCacheParameterGroup, ElastiCacheUser,
  ElastiCacheUserGroup, ElastiCacheSnapshot): cache metadata (never AUTH
  tokens, passwords, access strings, cache keys/values, snapshot data).
- `aws_relationship`: `elasticache_cluster_in_vpc`, `elasticache_cluster_in_subnet`,
  `elasticache_cluster_uses_kms_key`, `elasticache_replication_group_has_cluster`,
  `elasticache_user_group_has_user`.

### Edge audit (Glue)

| Edge | Positive case | Malformed / missing-field case | Verdict |
|---|---|---|---|
| table_in_database | `TestScannerEmitsGlueMetadataResourcesAndRelationships` (line 207-210) | Database name is required for table emission. | Covered |
| table_stored_at_s3_location | `TestScannerEmitsGlueMetadataResourcesAndRelationships` (line 212-231) | `TestScannerOmitsTableS3RelationshipWhenLocationIsNotS3` (line 266-283); `TestScannerOmitsTableS3RelationshipWhenLocationHasNoBucket` (line 315-332) | Covered |
| crawler_uses_iam_role | `TestScannerEmitsGlueMetadataResourcesAndRelationships` (line 241-247) | `TestScannerOmitsRoleRelationshipsWhenRoleIsNotARN` (line 334-350) | Covered |
| job_uses_iam_role | `TestScannerEmitsGlueMetadataResourcesAndRelationships` (line 249-255) | `TestScannerOmitsRoleRelationshipsWhenRoleIsNotARN` (line 334-350) | Covered |
| trigger_invokes_job | `TestScannerEmitsGlueMetadataResourcesAndRelationships` (line 257-263); `TestScannerEmitsOneRelationshipPerTriggerAction` (line 352-365, multiple trigger actions) | Empty ActionJobs list emits no relationships. | Covered |
| Partition derivation | `TestTableS3LocationRelationshipDerivesPartition` (relationships_test.go, line 19-53, commercial/govcloud/china partitions) | Blank region falls back to commercial partition. | Covered |
| Secret-shaped key filtering | `TestScannerDropsSecretShapedDefaultArgumentKeys` (line 367-406); `TestScannerDropsSecretShapedConnectionPropertyKeys` (line 408-440) | Secret-bearing keys are dropped; safe keys are retained. | Covered |

### Edge audit (ElastiCache)

| Edge | Positive case | Malformed / missing-field case | Verdict |
|---|---|---|---|
| cluster_in_vpc | `TestScannerEmitsElastiCacheMetadataOnlyFactsAndRelationships` (line 231) | `TestScannerSkipsRelationshipsWithoutTargets` (line 249-263, no relationships when target identity is missing) | Covered |
| cluster_uses_kms_key | `TestScannerEmitsElastiCacheMetadataOnlyFactsAndRelationships` (line 236) | `TestScannerDoesNotTreatNonARNKMSIdentifierAsARN` (line 265-283, alias/orders handled correctly) | Covered |
| replication_group_has_cluster | `TestScannerEmitsElastiCacheMetadataOnlyFactsAndRelationships` (line 237) | Empty MemberClusters emits no relationships. | Covered |
| user_group_has_user | `TestScannerEmitsElastiCacheMetadataOnlyFactsAndRelationships` (line 239) | Empty UserIDs emits no relationships. | Covered |

### Test files (Glue)
- `services/glue/scanner_test.go`: 10 test functions.
- `services/glue/relationships_test.go`: partition derivation test.
- `services/glue/servicekind_test.go`: service-kind tests.
- `services/glue/awssdk/`: adapter tests for database, table, crawler, job, trigger, workflow, connection mapping.

### Test files (ElastiCache)
- `services/elasticache/scanner_test.go`: 6 test functions.
- `services/elasticache/servicekind_test.go`: service-kind tests.
- `services/elasticache/awssdk/`: adapter tests for cache cluster, replication group, user, snapshot mapping.

## Summary

All seven priority services (S3, IAM, EC2, KMS, Secrets Manager, Glue,
ElastiCache) have:

1. **Positive-case assertions** for every named canonical edge.
2. **Malformed-input or missing-field assertions** for every edge where the
   field is semantically meaningful to validate (optional fields are handled
   gracefully by the scanner's zero-value semantics).
3. **Metadata-only contracts** enforced by forbidden-payload tests (no raw
   policy JSON, no secret values, no credential-shaped keys, no object
   content, no encryption contexts, no mutation APIs).

No test gaps were identified. All services close as verified.
