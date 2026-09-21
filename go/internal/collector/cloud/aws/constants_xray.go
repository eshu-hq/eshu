// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceXRay identifies the regional AWS X-Ray metadata-only scan slice.
	// The scanner emits X-Ray configuration only: trace groups, sampling rules,
	// and the account-region encryption configuration. It never reads or
	// persists observability payload — traces, trace summaries, segments, or
	// service-graph (service-map) data are outside the scan slice by
	// construction (the SDK adapter read surface excludes GetTraceSummaries,
	// BatchGetTraces, GetTraceGraph, GetServiceGraph, GetTimeSeriesService
	// Statistics, GetInsight*, and every Put*/Create*/Update*/Delete* mutation).
	ServiceXRay = "xray"
)

const (
	// ResourceTypeXRayGroup identifies an X-Ray group configuration resource.
	// The scanner persists the group name, ARN, and trace filter expression
	// (a configuration string, not trace data) plus insights-enablement flags.
	// Traces selected by the filter expression are never read.
	ResourceTypeXRayGroup = "aws_xray_group"
	// ResourceTypeXRaySamplingRule identifies an X-Ray sampling rule
	// configuration resource. The scanner persists the rule name, ARN,
	// priority, reservoir size, fixed rate, and the service name/type/host
	// match criteria. These describe which requests X-Ray samples; no sampled
	// trace, segment, or summary is read.
	ResourceTypeXRaySamplingRule = "aws_xray_sampling_rule"
	// ResourceTypeXRayEncryptionConfig identifies the account-region X-Ray
	// encryption configuration resource. X-Ray exposes a single encryption
	// configuration per account and region (no ARN), so the scanner keys it by
	// a synthetic "<account>/<region>/xray-encryption-config" resource id. It
	// carries the encryption type (NONE or KMS), status, and the configured
	// KMS key reference only.
	ResourceTypeXRayEncryptionConfig = "aws_xray_encryption_config"
	// ResourceTypeXRayServiceCorrelation identifies the synthetic service
	// identity an X-Ray sampling rule matches by service name and service
	// type. X-Ray sampling rules name a service by the string it reports in
	// segments (ServiceName) and its origin type (ServiceType); this is a
	// labeled correlation anchor, not a scanned AWS resource, so reducers join
	// it to the real service node by name during materialization. Declaring it
	// as a ResourceType constant keeps the relationship graph-join contract
	// satisfied without a fabricated ARN.
	ResourceTypeXRayServiceCorrelation = "aws_xray_service_correlation"
)

const (
	// RelationshipXRayEncryptionConfigUsesKMSKey records the KMS key the X-Ray
	// account-region encryption configuration uses, emitted only when the
	// encryption type is KMS and AWS reports a key reference. The target is the
	// KMS key family (aws_kms_key); the join key is the reported key
	// id/ARN/alias, matching how the KMS scanner publishes its key resource id.
	RelationshipXRayEncryptionConfigUsesKMSKey = "xray_encryption_config_uses_kms_key"
	// RelationshipXRaySamplingRuleMatchesService records the service identity a
	// sampling rule matches by service name and service type. It is a labeled
	// correlation anchor (target_type aws_xray_service_correlation): the rule
	// names the service by the string it reports in segments, and the reducer
	// resolves it to the real service node by name during materialization.
	RelationshipXRaySamplingRuleMatchesService = "xray_sampling_rule_matches_service"
)
