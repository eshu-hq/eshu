// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpolicy

import "github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"

// Policy returns the AWS record-mode field table: every payload key of the
// aws/v1 fact schemas (sdk/go/factschema/schema/aws_*.json), the scope
// metadata keys, and the attribute keys of the services in the committed
// corpus, each mapped to the pseudonym class its consumers can tolerate. The
// table is copied on every call so a caller can adjust it without touching
// the shared one.
//
// A key absent here is unclassified: the engine makes its values opaque and
// reports the path (fail closed). The one deliberately unlisted corpus key is
// attributes.containers[].runtime_id, an ECS container runtime id no reducer
// reads; opaque is the right answer and its path is expected in the record
// report.
func Policy() recordpseudo.Policy {
	fields := make(map[string]recordpseudo.Class, len(accountKeys)+len(arnKeys)+len(identKeys)+len(awsIDKeys)+len(hostKeys)+len(imageRefKeys)+len(ipv4Keys)+len(keepKeys)+len(opaqueKeys)+2)
	set := func(keys []string, class recordpseudo.Class) {
		for _, key := range keys {
			fields[key] = class
		}
	}
	set(keepKeys, recordpseudo.ClassKeep)
	set(accountKeys, recordpseudo.ClassAccount)
	set(arnKeys, recordpseudo.ClassARN)
	set(identKeys, recordpseudo.ClassIdent)
	set(awsIDKeys, recordpseudo.ClassAWSID)
	set(hostKeys, recordpseudo.ClassHost)
	set(imageRefKeys, recordpseudo.ClassECRRef)
	set(ipv4Keys, recordpseudo.ClassIPv4)
	set(opaqueKeys, recordpseudo.ClassOpaque)
	fields["tags"] = recordpseudo.ClassTagValue
	fields["tag"] = recordpseudo.ClassImageTag       // ECR image tag: latest/semver kept, customer tags become names
	fields["source_value"] = recordpseudo.ClassIdent // CIDR, security-group id or prefix-list id: shape-sniffed
	return recordpseudo.Policy{Fields: fields}
}

// accountKeys carry a 12-digit account id (or a list of them).
var accountKeys = []string{
	"account_id", "registry_id", "group_owner_id", "principal_account_id", "principal_account_ids",
}

// arnKeys always carry an ARN or a list of ARNs.
var arnKeys = []string{
	"arn", "source_arn", "target_arn", "cluster_arn", "task_definition_arn", "task_role_arn",
	"execution_role_arn", "role_arn", "role_arns", "principal_arn", "principal_arns", "repository_arn",
	"bucket_arn", "instance_profile_arn", "profile_arn", "sse_kms_key_arn", "resources", "not_resources",
	"correlation_anchors", "analyzer_arn", "boundary_policy_arn", "policy_arn", "resource_arn",
}

// identKeys carry names or composite ids whose shape the engine sniffs
// (ARN, ECR reference, AWS-issued id, address, hostname, name:revision).
var identKeys = []string{
	"name", "repository_name", "bucket_name", "policy_name", "collector_instance_id", "resource_id",
	"source_resource_id", "target_resource_id", "workload_id", "group", "container_names", "identifier",
	"ca_certificate_identifier", "health_check_id", "traffic_policy_instance_id", "set_identifier",
	"cidr_collection_id", "cidr_location_name", "logging_target_bucket", "option_groups", "parameter_groups",
	"source_statement_id", "statement_sid", "principal_value", "principal_id", "kms_key_id",
	"performance_insights_kms_key_id", "hosted_zone_id", "correlation_hints", "assume_principals",
	"finding_id", "path", "values",
}

// awsIDKeys carry AWS-issued prefixed ids (i-, ami-, subnet-, sg-, eni-, vol-, ...).
var awsIDKeys = []string{
	"instance_id", "ami_id", "network_interface_id", "subnet_id", "group_id", "volume_id", "rule_id",
}

// hostKeys carry DNS names.
var hostKeys = []string{
	"dns_name", "normalized_dns_name", "record_name", "normalized_record_name", "hosted_zone_name",
	"source_hosted_zone_name",
}

// imageRefKeys carry container image references (registry host, repository
// path, tag or digest).
var imageRefKeys = []string{"image", "image_uri", "resolved_image_uri", "uri"}

// ipv4Keys carry a single IPv4 address.
var ipv4Keys = []string{"private_ipv4_address", "public_ip_address"}

// opaqueKeys are free text that no consumer parses: replaced wholesale.
var opaqueKeys = []string{"description", "message", "unsupported_key"}

// keepKeys are enums, structure, digests, timestamps, numbers and booleans.
// Nested objects (attributes, alias_target, geo_location, containers, ...)
// are Keep at the object key: the engine recurses and classifies each child
// by its own key.
var keepKeys = []string{
	"region", "service_kind", "resource_type", "state", "relationship_type", "target_type", "principal_type",
	"principal_types", "provider", "policy_source", "effect", "actions", "not_actions", "package_type", "version",
	"launch_type", "desired_status", "image_tag_mutability", "tenancy", "redaction_policy_version", "warning_kind",
	"source_state", "environment", "image_digest", "manifest_digest", "code_sha256", "started_at", "cpu",
	"memory", "web_identity_subject_fingerprints", "web_identity_subject_wildcard", "schema_version",
	"resource_scope", "error_class", "status", "record_type", "routing_policy", "direction", "ip_protocol",
	"from_port", "to_port", "principal_kind", "principal_partition", "principal_service", "grant_outcome",
	"resolution_mode", "source_kind", "target_identity_family", "target_identity_version", "manifest_media_type",
	"artifact_media_type", "pushed_at", "ttl", "weight", "failover", "continent_code", "country_code",
	"subdivision_code", "geo_location", "alias_target", "evaluate_target_health", "encryption_algorithms",
	"backup_retention_period", "engine", "device_name", "block_devices", "delete_on_termination", "encrypted",
	"attributes", "containers", "network_interfaces", "security_parameters", "boundary_type", "client_id_count",
	"condition_keys", "condition_operator_count", "condition_operators", "finding_type", "has_conditions",
	"is_wildcard_action", "is_wildcard_resource", "role_count", "thumbprint_count", "url_fingerprint",
	"url_present", "hosted_zone_private", "image_size_in_bytes", "has_alias_target", "multi_value_answer",
	"is_all_ports", "is_all_protocols", "is_cross_account", "is_internet", "is_public", "is_service_principal",
	"is_unsupported", "acl_disabled", "block_public_access_all", "block_public_acls", "block_public_policy",
	"bucket_key_enabled", "default_encryption_enabled", "deletion_protection", "detailed_monitoring_enabled",
	"ebs_optimized", "iam_database_authentication_enabled", "imds_http_endpoint", "imds_http_put_hop_limit",
	"imds_v2_required", "logging_enabled", "mfa_delete_enabled", "multi_az", "nitro_enclave_enabled",
	"object_ownership", "performance_insights_enabled", "performance_insights_retention_days",
	"policy_grants_cross_account", "policy_grants_public", "policy_present", "public_ip_associated",
	"publicly_accessible", "replication_enabled", "restrict_public_buckets", "storage_encrypted", "user_data_present",
	"versioning_enabled", "versioning_status", "ignore_public_acls",
}
