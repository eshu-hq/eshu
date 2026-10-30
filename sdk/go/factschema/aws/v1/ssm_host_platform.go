// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "strings"

// Closed platform-evidence provenances for SSMHostPlatform.SourceType
// (eshu-hq/eshu#9556, accepted record #9302). Inventory-sourced platform
// evidence carries the inventory capture time; DescribeInstanceInformation
// evidence carries only its observation time with that provenance — the
// collector must never invent a capture time for it.
const (
	// SSMHostPlatformSourceInventory names platform evidence read from SSM
	// inventory alongside the application capture.
	SSMHostPlatformSourceInventory = "ssm_inventory"

	// SSMHostPlatformSourceDescribeInstanceInformation names platform
	// evidence read from DescribeInstanceInformation, which has no
	// inventory capture time.
	SSMHostPlatformSourceDescribeInstanceInformation = "describe_instance_information"
)

// SSMHostPlatform is the schema-version-1 typed payload for the
// "aws_ssm_host_platform" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issues #9302 and #9556).
//
// One fact is the platform observation for one EC2 managed node: the host
// tuple (Partition, AccountID, Region, InstanceID, ResourceType), the
// platform fields (PlatformType, PlatformName, PlatformVersion) when
// present, the source type with its provenance, and the observation or
// capture time. It is identity and version-matching evidence, not a
// vulnerability finding: no advisory comparison may read platform matching
// from this fact alone. A complete newer generation updates the platform
// observation.
type SSMHostPlatform struct {
	// Partition is the AWS partition the managed node was observed in.
	// Required.
	Partition string `json:"partition"`

	// AccountID is the AWS account the managed node was observed in.
	// Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the managed node was observed in. Required.
	Region string `json:"region"`

	// InstanceID is the EC2 managed-node instance identifier. Required.
	InstanceID string `json:"instance_id"`

	// ResourceType is the observed resource type, which must be the EC2
	// instance type for this kind. Required.
	ResourceType string `json:"resource_type"`

	// PlatformType is the reported platform type (for example Linux),
	// when present.
	PlatformType *string `json:"platform_type,omitempty"`

	// PlatformName is the reported platform name (for example AmazonLinux),
	// when present.
	PlatformName *string `json:"platform_name,omitempty"`

	// PlatformVersion is the reported platform version, when present.
	PlatformVersion *string `json:"platform_version,omitempty"`

	// SourceType is the platform evidence provenance: one of
	// SSMHostPlatformSourceInventory or
	// SSMHostPlatformSourceDescribeInstanceInformation. Required.
	SourceType string `json:"source_type"`

	// CaptureTime is the SSM inventory capture time (RFC 3339) for
	// inventory-sourced platform evidence. Optional: nil for
	// DescribeInstanceInformation observations, which have no inventory
	// capture time — a value here with that provenance is a contract
	// violation ValidateSSMHostPlatformProvenance rejects; the satellite
	// emitter calls it before building platform facts.
	CaptureTime *string `json:"capture_time,omitempty"`

	// ObservationTime is when this platform evidence was observed
	// (RFC 3339). Required: for DescribeInstanceInformation evidence this
	// is the evidence time, and it must not be labeled inventory capture
	// time.
	ObservationTime string `json:"observation_time"`

	// CollectorInstanceID identifies the collector instance run that emitted
	// this fact. Optional: it is operational metadata about the run, not part
	// of the observation's identity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// SSMHostPlatformStableKey returns the stable key for one platform
// observation: host tuple plus source type. A complete newer generation
// updates the platform observation on this key; a DescribeInstanceInformation
// observation time must not be labeled inventory capture time anywhere in
// the key.
func SSMHostPlatformStableKey(platform SSMHostPlatform) string {
	return strings.Join([]string{
		"aws_ssm_host_platform",
		platform.Partition,
		platform.AccountID,
		platform.Region,
		platform.InstanceID,
		platform.SourceType,
	}, "/")
}
