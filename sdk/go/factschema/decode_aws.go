// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// DecodeAWSResource decodes env.Payload into the latest awsv1.Resource struct
// for the "aws_resource" fact kind, dispatching on env.SchemaVersion major per
// Contract System v1 §3.2. Callers (reducer handlers) receive either the decoded
// struct or a classified *DecodeError; they must never substitute a zero-value
// struct on error.
func DecodeAWSResource(env Envelope) (awsv1.Resource, error) {
	return decodeLatestMajor[awsv1.Resource](FactKindAWSResource, env)
}

// EncodeAWSResource marshals an awsv1.Resource into the map[string]any payload
// shape an Envelope carries. It is the inverse of DecodeAWSResource for
// schema-version-1 payloads, used by collectors emitting this fact kind and by
// this module's round-trip tests.
func EncodeAWSResource(resource awsv1.Resource) (map[string]any, error) {
	payload := map[string]any{
		"account_id":    resource.AccountID,
		"resource_id":   resource.ResourceID,
		"region":        resource.Region,
		"resource_type": resource.ResourceType,
	}
	addStringPtr(payload, "arn", resource.ARN)
	addStringPtr(payload, "name", resource.Name)
	addStringPtr(payload, "state", resource.State)
	addStringPtr(payload, "service_kind", resource.ServiceKind)
	addStringSlice(payload, "correlation_anchors", resource.CorrelationAnchors)
	addStringMapPtr(payload, "tags", resource.Tags)
	mergeUnknownPayloadKeys(payload, resource.Attributes, awsResourcePayloadKeys)
	return payload, nil
}

// DecodeAWSRelationship decodes env.Payload into the latest awsv1.Relationship
// struct for the "aws_relationship" fact kind. See DecodeAWSResource for the
// dispatch and error contract.
func DecodeAWSRelationship(env Envelope) (awsv1.Relationship, error) {
	return decodeLatestMajor[awsv1.Relationship](FactKindAWSRelationship, env)
}

// EncodeAWSRelationship marshals an awsv1.Relationship into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeAWSRelationship
// for schema-version-1 payloads.
func EncodeAWSRelationship(relationship awsv1.Relationship) (map[string]any, error) {
	payload := map[string]any{
		"account_id":         relationship.AccountID,
		"region":             relationship.Region,
		"relationship_type":  relationship.RelationshipType,
		"source_resource_id": relationship.SourceResourceID,
		"target_resource_id": relationship.TargetResourceID,
	}
	addStringPtr(payload, "source_arn", relationship.SourceARN)
	addStringPtr(payload, "target_arn", relationship.TargetARN)
	addStringPtr(payload, "target_type", relationship.TargetType)
	mergeUnknownPayloadKeys(payload, relationship.Attributes, awsRelationshipPayloadKeys)
	return payload, nil
}

// DecodeAWSDNSRecord decodes env.Payload into the latest awsv1.DNSRecord
// struct for the "aws_dns_record" fact kind.
func DecodeAWSDNSRecord(env Envelope) (awsv1.DNSRecord, error) {
	return decodeLatestMajor[awsv1.DNSRecord](FactKindAWSDNSRecord, env)
}

// EncodeAWSDNSRecord maps an awsv1.DNSRecord directly to an Envelope payload.
func EncodeAWSDNSRecord(record awsv1.DNSRecord) (map[string]any, error) {
	payload := make(map[string]any, 16)
	payload["account_id"] = record.AccountID
	payload["region"] = record.Region
	payload["hosted_zone_id"] = record.HostedZoneID
	payload["record_name"] = record.RecordName
	payload["normalized_record_name"] = record.NormalizedRecordName
	payload["record_type"] = record.RecordType
	addStringPtr(payload, "service_kind", record.ServiceKind)
	addStringPtr(payload, "collector_instance_id", record.CollectorInstanceID)
	addStringPtr(payload, "hosted_zone_name", record.HostedZoneName)
	addBoolPtr(payload, "hosted_zone_private", record.HostedZonePrivate)
	addStringPtr(payload, "set_identifier", record.SetIdentifier)
	addInt64Ptr(payload, "ttl", record.TTL)
	addStringSlice(payload, "values", record.Values)
	if record.AliasTarget != nil {
		payload["alias_target"] = encodeDNSAliasTarget(*record.AliasTarget)
	}
	if record.RoutingPolicy != nil {
		payload["routing_policy"] = encodeDNSRoutingPolicy(*record.RoutingPolicy)
	}
	addStringSlice(payload, "correlation_anchors", record.CorrelationAnchors)
	addBoolPtr(payload, "has_alias_target", record.HasAliasTarget)
	addStringPtr(payload, "source_hosted_zone_name", record.SourceHostedZoneName)
	return payload, nil
}

// DecodeAWSImageReference decodes env.Payload into the latest
// awsv1.ImageReference struct for the "aws_image_reference" fact kind.
func DecodeAWSImageReference(env Envelope) (awsv1.ImageReference, error) {
	return decodeLatestMajor[awsv1.ImageReference](FactKindAWSImageReference, env)
}

// EncodeAWSImageReference maps an awsv1.ImageReference directly to a payload.
func EncodeAWSImageReference(reference awsv1.ImageReference) (map[string]any, error) {
	payload := map[string]any{
		"account_id":      reference.AccountID,
		"region":          reference.Region,
		"repository_name": reference.RepositoryName,
		"image_digest":    reference.ImageDigest,
		"manifest_digest": reference.ManifestDigest,
	}
	addStringPtr(payload, "service_kind", reference.ServiceKind)
	addStringPtr(payload, "collector_instance_id", reference.CollectorInstanceID)
	addStringPtr(payload, "repository_arn", reference.RepositoryARN)
	addStringPtr(payload, "registry_id", reference.RegistryID)
	addStringPtr(payload, "tag", reference.Tag)
	addTimePtr(payload, "pushed_at", reference.PushedAt)
	addInt64Ptr(payload, "image_size_in_bytes", reference.ImageSizeInBytes)
	addStringPtr(payload, "manifest_media_type", reference.ManifestMediaType)
	addStringPtr(payload, "artifact_media_type", reference.ArtifactMediaType)
	addStringSlice(payload, "correlation_anchors", reference.CorrelationAnchors)
	return payload, nil
}

// DecodeAWSSecurityGroupRule decodes env.Payload into the latest
// awsv1.SecurityGroupRule struct for the "aws_security_group_rule" fact kind.
// See DecodeAWSResource for the dispatch and error contract.
func DecodeAWSSecurityGroupRule(env Envelope) (awsv1.SecurityGroupRule, error) {
	return decodeLatestMajor[awsv1.SecurityGroupRule](FactKindAWSSecurityGroupRule, env)
}

// EncodeAWSSecurityGroupRule marshals an awsv1.SecurityGroupRule into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAWSSecurityGroupRule for schema-version-1 payloads.
func EncodeAWSSecurityGroupRule(rule awsv1.SecurityGroupRule) (map[string]any, error) {
	payload := map[string]any{
		"account_id":   rule.AccountID,
		"region":       rule.Region,
		"group_id":     rule.GroupID,
		"direction":    rule.Direction,
		"ip_protocol":  rule.IPProtocol,
		"source_kind":  rule.SourceKind,
		"source_value": rule.SourceValue,
	}
	addStringPtr(payload, "service_kind", rule.ServiceKind)
	addStringPtr(payload, "collector_instance_id", rule.CollectorInstanceID)
	addStringPtr(payload, "rule_id", rule.RuleID)
	addStringPtr(payload, "group_owner_id", rule.GroupOwnerID)
	addStringPtr(payload, "description", rule.Description)
	addInt32Ptr(payload, "from_port", rule.FromPort)
	addInt32Ptr(payload, "to_port", rule.ToPort)
	addBoolPtr(payload, "is_internet", rule.IsInternet)
	addBoolPtr(payload, "is_all_protocols", rule.IsAllProtocols)
	addBoolPtr(payload, "is_all_ports", rule.IsAllPorts)
	addStringSlice(payload, "correlation_anchors", rule.CorrelationAnchors)
	return payload, nil
}

// DecodeAWSWarning decodes env.Payload into the latest awsv1.Warning struct for
// the "aws_warning" fact kind.
func DecodeAWSWarning(env Envelope) (awsv1.Warning, error) {
	return decodeLatestMajor[awsv1.Warning](FactKindAWSWarning, env)
}

// EncodeAWSWarning maps an awsv1.Warning directly to an Envelope payload.
func EncodeAWSWarning(warning awsv1.Warning) (map[string]any, error) {
	payload := map[string]any{
		"account_id":   warning.AccountID,
		"region":       warning.Region,
		"warning_kind": warning.WarningKind,
	}
	addStringPtr(payload, "service_kind", warning.ServiceKind)
	addStringPtr(payload, "collector_instance_id", warning.CollectorInstanceID)
	addStringPtr(payload, "error_class", warning.ErrorClass)
	addStringPtr(payload, "message", warning.Message)
	addAnyMap(payload, "attributes", warning.Attributes)
	return payload, nil
}

// DecodeEC2InstancePosture decodes env.Payload into the latest
// awsv1.EC2InstancePosture struct for the "ec2_instance_posture" fact kind. See
// DecodeAWSResource for the dispatch and error contract.
func DecodeEC2InstancePosture(env Envelope) (awsv1.EC2InstancePosture, error) {
	return decodeLatestMajor[awsv1.EC2InstancePosture](FactKindEC2InstancePosture, env)
}

// EncodeEC2InstancePosture marshals an awsv1.EC2InstancePosture into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeEC2InstancePosture for schema-version-1 payloads.
func EncodeEC2InstancePosture(posture awsv1.EC2InstancePosture) (map[string]any, error) {
	payload := map[string]any{
		"account_id": posture.AccountID,
		"region":     posture.Region,
	}
	addStringPtr(payload, "instance_id", posture.InstanceID)
	addStringPtr(payload, "arn", posture.ARN)
	addStringPtr(payload, "resource_type", posture.ResourceType)
	addStringPtr(payload, "service_kind", posture.ServiceKind)
	addStringPtr(payload, "collector_instance_id", posture.CollectorInstanceID)
	addStringPtr(payload, "state", posture.State)
	addBoolPtr(payload, "imds_v2_required", posture.IMDSv2Required)
	addStringPtr(payload, "imds_http_endpoint", posture.IMDSHTTPEndpoint)
	addInt32Ptr(payload, "imds_http_put_hop_limit", posture.IMDSHTTPPutHopLimit)
	addBoolPtr(payload, "user_data_present", posture.UserDataPresent)
	addBoolPtr(payload, "detailed_monitoring_enabled", posture.DetailedMonitoringEnabled)
	addBoolPtr(payload, "ebs_optimized", posture.EBSOptimized)
	addBoolPtr(payload, "public_ip_associated", posture.PublicIPAssociated)
	addStringPtr(payload, "public_ip_address", posture.PublicIPAddress)
	addStringPtr(payload, "instance_profile_arn", posture.InstanceProfileARN)
	addStringPtr(payload, "tenancy", posture.Tenancy)
	addBoolPtr(payload, "nitro_enclave_enabled", posture.NitroEnclaveEnabled)
	if posture.BlockDevices != nil {
		payload["block_devices"] = encodeBlockDevices(posture.BlockDevices)
	}
	addStringSlice(payload, "correlation_anchors", posture.CorrelationAnchors)
	return payload, nil
}

// DecodeRDSInstancePosture decodes env.Payload into awsv1.RDSInstancePosture.
func DecodeRDSInstancePosture(env Envelope) (awsv1.RDSInstancePosture, error) {
	return decodeLatestMajor[awsv1.RDSInstancePosture](FactKindRDSInstancePosture, env)
}

// EncodeRDSInstancePosture maps an awsv1.RDSInstancePosture directly to a payload.
func EncodeRDSInstancePosture(posture awsv1.RDSInstancePosture) (map[string]any, error) {
	payload := map[string]any{
		"account_id":                          posture.AccountID,
		"region":                              posture.Region,
		"publicly_accessible":                 posture.PubliclyAccessible,
		"storage_encrypted":                   posture.StorageEncrypted,
		"iam_database_authentication_enabled": posture.IAMDatabaseAuthenticationEnabled,
		"multi_az":                            posture.MultiAZ,
		"deletion_protection":                 posture.DeletionProtection,
		"backup_retention_period":             posture.BackupRetentionPeriod,
		"performance_insights_enabled":        posture.PerformanceInsightsEnabled,
		"performance_insights_retention_days": posture.PerformanceInsightsRetentionDays,
	}
	addStringPtr(payload, "service_kind", posture.ServiceKind)
	addStringPtr(payload, "collector_instance_id", posture.CollectorInstanceID)
	addStringPtr(payload, "arn", posture.ARN)
	addStringPtr(payload, "resource_id", posture.ResourceID)
	addStringPtr(payload, "resource_type", posture.ResourceType)
	addStringPtr(payload, "identifier", posture.Identifier)
	addStringPtr(payload, "engine", posture.Engine)
	addStringPtr(payload, "kms_key_id", posture.KMSKeyID)
	addStringPtr(payload, "performance_insights_kms_key_id", posture.PerformanceInsightsKMSKeyID)
	addStringPtr(payload, "ca_certificate_identifier", posture.CACertificateIdentifier)
	addStringSlice(payload, "parameter_groups", posture.ParameterGroups)
	addStringSlice(payload, "option_groups", posture.OptionGroups)
	if posture.SecurityParameters != nil {
		payload["security_parameters"] = *posture.SecurityParameters
	}
	addStringSlice(payload, "correlation_anchors", posture.CorrelationAnchors)
	return payload, nil
}

// DecodeS3BucketPosture decodes env.Payload into the latest
// awsv1.S3BucketPosture struct for the "s3_bucket_posture" fact kind. See
// DecodeAWSResource for the dispatch and error contract.
func DecodeS3BucketPosture(env Envelope) (awsv1.S3BucketPosture, error) {
	return decodeLatestMajor[awsv1.S3BucketPosture](FactKindS3BucketPosture, env)
}

// EncodeS3BucketPosture marshals an awsv1.S3BucketPosture into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeS3BucketPosture for schema-version-1 payloads.
func EncodeS3BucketPosture(posture awsv1.S3BucketPosture) (map[string]any, error) {
	payload := map[string]any{
		"account_id": posture.AccountID,
		"region":     posture.Region,
	}
	addStringPtr(payload, "service_kind", posture.ServiceKind)
	addStringPtr(payload, "collector_instance_id", posture.CollectorInstanceID)
	addStringPtr(payload, "bucket_arn", posture.BucketARN)
	addStringPtr(payload, "bucket_name", posture.BucketName)
	addStringPtr(payload, "logging_target_bucket", posture.LoggingTargetBucket)
	addBoolPtr(payload, "policy_present", posture.PolicyPresent)
	addBoolPtr(payload, "policy_grants_public", posture.PolicyGrantsPublic)
	addBoolPtr(payload, "block_public_access_all", posture.BlockPublicAccessAll)
	addBoolPtr(payload, "ignore_public_acls", posture.IgnorePublicACLs)
	addBoolPtr(payload, "restrict_public_buckets", posture.RestrictPublicBuckets)
	addBoolPtr(payload, "block_public_acls", posture.BlockPublicACLs)
	addBoolPtr(payload, "block_public_policy", posture.BlockPublicPolicy)
	addBoolPtr(payload, "default_encryption_enabled", posture.DefaultEncryptionEnabled)
	addStringSlice(payload, "encryption_algorithms", posture.EncryptionAlgorithms)
	addStringPtr(payload, "sse_kms_key_arn", posture.SSEKMSKeyARN)
	addBoolPtr(payload, "bucket_key_enabled", posture.BucketKeyEnabled)
	addStringPtr(payload, "versioning_status", posture.VersioningStatus)
	addBoolPtr(payload, "versioning_enabled", posture.VersioningEnabled)
	addBoolPtr(payload, "mfa_delete_enabled", posture.MFADeleteEnabled)
	addStringSlice(payload, "object_ownership", posture.ObjectOwnership)
	addBoolPtr(payload, "acl_disabled", posture.ACLDisabled)
	addBoolPtr(payload, "logging_enabled", posture.LoggingEnabled)
	addBoolPtr(payload, "replication_enabled", posture.ReplicationEnabled)
	addBoolPtr(payload, "policy_grants_cross_account", posture.PolicyGrantsCrossAccount)
	addStringSlice(payload, "correlation_anchors", posture.CorrelationAnchors)
	return payload, nil
}

// DecodeS3ExternalPrincipalGrant decodes env.Payload into the latest
// awsv1.S3ExternalPrincipalGrant struct.
func DecodeS3ExternalPrincipalGrant(env Envelope) (awsv1.S3ExternalPrincipalGrant, error) {
	return decodeLatestMajor[awsv1.S3ExternalPrincipalGrant](FactKindS3ExternalPrincipalGrant, env)
}

// EncodeS3ExternalPrincipalGrant maps an awsv1.S3ExternalPrincipalGrant to a payload.
func EncodeS3ExternalPrincipalGrant(grant awsv1.S3ExternalPrincipalGrant) (map[string]any, error) {
	payload := map[string]any{
		"account_id":           grant.AccountID,
		"region":               grant.Region,
		"principal_kind":       grant.PrincipalKind,
		"principal_value":      grant.PrincipalValue,
		"grant_outcome":        grant.GrantOutcome,
		"is_public":            grant.IsPublic,
		"is_cross_account":     grant.IsCrossAccount,
		"is_service_principal": grant.IsServicePrincipal,
		"is_unsupported":       grant.IsUnsupported,
	}
	addStringPtr(payload, "service_kind", grant.ServiceKind)
	addStringPtr(payload, "collector_instance_id", grant.CollectorInstanceID)
	addStringPtr(payload, "bucket_arn", grant.BucketARN)
	addStringPtr(payload, "bucket_name", grant.BucketName)
	addStringPtr(payload, "principal_account_id", grant.PrincipalAccountID)
	addStringPtr(payload, "principal_partition", grant.PrincipalPartition)
	addStringPtr(payload, "principal_service", grant.PrincipalService)
	addStringPtr(payload, "unsupported_key", grant.UnsupportedKey)
	addStringPtr(payload, "source_statement_id", grant.SourceStatementID)
	addStringPtr(payload, "resolution_mode", grant.ResolutionMode)
	addStringSlice(payload, "correlation_anchors", grant.CorrelationAnchors)
	return payload, nil
}

var awsResourcePayloadKeys = map[string]struct{}{
	"account_id": {}, "resource_id": {}, "region": {}, "resource_type": {},
	"arn": {}, "name": {}, "state": {}, "service_kind": {},
	"correlation_anchors": {}, "tags": {},
}

var awsRelationshipPayloadKeys = map[string]struct{}{
	"account_id": {}, "region": {}, "relationship_type": {},
	"source_resource_id": {}, "target_resource_id": {},
	"source_arn": {}, "target_arn": {}, "target_type": {},
}

func mergeUnknownPayloadKeys(payload map[string]any, attributes map[string]any, known map[string]struct{}) {
	for key, value := range attributes {
		if _, exists := known[key]; exists {
			continue
		}
		if _, exists := payload[key]; exists {
			continue
		}
		payload[key] = value
	}
}

func encodeDNSAliasTarget(target awsv1.DNSAliasTarget) map[string]any {
	return map[string]any{
		"dns_name":                target.DNSName,
		"normalized_dns_name":     target.NormalizedDNSName,
		"hosted_zone_id":          target.HostedZoneID,
		"evaluate_target_health":  target.EvaluateTargetHealth,
		"target_identity_family":  target.TargetIdentityFamily,
		"target_identity_version": target.TargetIdentityVersion,
	}
}

func encodeDNSRoutingPolicy(policy awsv1.DNSRoutingPolicy) map[string]any {
	payload := make(map[string]any)
	addInt64Ptr(payload, "weight", policy.Weight)
	addStringPtr(payload, "region", policy.Region)
	addStringPtr(payload, "failover", policy.Failover)
	addStringPtr(payload, "health_check_id", policy.HealthCheckID)
	addBoolPtr(payload, "multi_value_answer", policy.MultiValueAnswer)
	addStringPtr(payload, "traffic_policy_instance_id", policy.TrafficPolicyInstanceID)
	if policy.GeoLocation != nil {
		payload["geo_location"] = encodeDNSGeoLocation(*policy.GeoLocation)
	}
	addStringPtr(payload, "cidr_collection_id", policy.CIDRCollectionID)
	addStringPtr(payload, "cidr_location_name", policy.CIDRLocationName)
	return payload
}

func encodeDNSGeoLocation(location awsv1.DNSGeoLocation) map[string]any {
	payload := make(map[string]any)
	addStringPtr(payload, "continent_code", location.ContinentCode)
	addStringPtr(payload, "country_code", location.CountryCode)
	addStringPtr(payload, "subdivision_code", location.SubdivisionCode)
	return payload
}

func encodeBlockDevices(devices []awsv1.BlockDevice) []map[string]any {
	out := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		payload := make(map[string]any)
		addStringPtr(payload, "device_name", device.DeviceName)
		addStringPtr(payload, "volume_id", device.VolumeID)
		addBoolPtr(payload, "delete_on_termination", device.DeleteOnTermination)
		addStringPtr(payload, "status", device.Status)
		addBoolPtr(payload, "encrypted", device.Encrypted)
		out = append(out, payload)
	}
	return out
}
