// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// EC2InstancePosture is the schema-version-1 typed payload for the
// "ec2_instance_posture" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// Only AccountID and Region are required: the collector emitter
// (awscloud.NewEC2InstancePostureEnvelope) validates instance_id OR arn
// non-empty as an either-or identity, so NEITHER InstanceID nor ARN can be
// required on its own — requiring one would dead-letter a valid fact identified
// only by the other. The reducer's source-uid derivation already tolerates a
// blank InstanceID by falling back to ARN, and skips (never dead-letters) a
// fact carrying neither. All other fields are optional posture properties the
// reducer copies onto the instance node or reads for edge derivation; each is a
// pointer or omitempty so an absent value stays distinct from an observed
// zero/false.
type EC2InstancePosture struct {
	// AccountID is the AWS account the instance was observed in. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the instance was observed in. Required.
	Region string `json:"region"`

	// CollectorInstanceID is the collector runtime instance that observed the
	// posture fact. Optional for generic schema decoding, but AWS collectors
	// must stamp it before emission.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// InstanceID is the bare EC2 instance id (i-...). Optional: the emitter
	// requires instance_id OR arn, so this may be empty when only the ARN was
	// observed; the reducer forms the source uid from ARN in that case.
	InstanceID *string `json:"instance_id,omitempty"`

	// ARN is the instance ARN. Optional: the emitter requires instance_id OR
	// arn, so this may be empty when only the bare id was observed.
	ARN *string `json:"arn,omitempty"`

	// ResourceType is the CloudResource resource-type token
	// (aws_ec2_instance). Optional: the reducer defaults it when absent, so an
	// empty value must not dead-letter.
	ResourceType *string `json:"resource_type,omitempty"`

	// ServiceKind is the collector service-kind boundary token. Optional
	// metadata copied onto the node.
	ServiceKind *string `json:"service_kind,omitempty"`

	// State is the instance lifecycle state string. Optional metadata.
	State *string `json:"state,omitempty"`

	// IMDSv2Required reports whether IMDSv2 is required. Optional pointer so
	// nil (unreported) stays distinct from an observed false.
	IMDSv2Required *bool `json:"imds_v2_required,omitempty"`

	// IMDSHTTPEndpoint is the IMDS HTTP endpoint state string. Optional.
	IMDSHTTPEndpoint *string `json:"imds_http_endpoint,omitempty"`

	// IMDSHTTPPutHopLimit is the IMDS put-response hop limit. Optional pointer
	// so nil (unreported) stays distinct from an observed zero.
	IMDSHTTPPutHopLimit *int32 `json:"imds_http_put_hop_limit,omitempty"`

	// UserDataPresent reports whether user data is present (presence only,
	// never content). Optional pointer preserving unreported vs observed false.
	UserDataPresent *bool `json:"user_data_present,omitempty"`

	// DetailedMonitoringEnabled reports detailed-monitoring state. Optional.
	DetailedMonitoringEnabled *bool `json:"detailed_monitoring_enabled,omitempty"`

	// EBSOptimized reports EBS-optimized state. Optional.
	EBSOptimized *bool `json:"ebs_optimized,omitempty"`

	// PublicIPAssociated reports whether a public IP is associated. Optional.
	PublicIPAssociated *bool `json:"public_ip_associated,omitempty"`

	// PublicIPAddress is the public IP address string when observed. Optional;
	// reducers intentionally do not project the raw value to graph properties.
	PublicIPAddress *string `json:"public_ip_address,omitempty"`

	// InstanceProfileARN is the attached IAM instance-profile ARN. Optional: a
	// blank value means no profile is attached (the normal no-edge state for
	// the USES_PROFILE join), not a malformed fact.
	InstanceProfileARN *string `json:"instance_profile_arn,omitempty"`

	// Tenancy is the instance tenancy string. Optional metadata.
	Tenancy *string `json:"tenancy,omitempty"`

	// NitroEnclaveEnabled reports Nitro-enclave state. Optional.
	NitroEnclaveEnabled *bool `json:"nitro_enclave_enabled,omitempty"`

	// BlockDevices holds per-volume block-device metadata the block-device KMS
	// posture consumer reads. Optional: an all-ports of instances omits it, and
	// the reducer treats an absent list as "no block devices".
	BlockDevices []BlockDevice `json:"block_devices,omitempty"`

	// CorrelationAnchors are the redaction-safe anchors the collector
	// published. Optional; the node copies them for name-only correlation.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// BlockDevice is one per-volume block-device metadata entry inside an
// EC2InstancePosture payload. Every field is optional: the collector emits the
// metadata it observed, and the block-device KMS posture reducer reads VolumeID
// and Encrypted while tolerating absent values (an unreported Encrypted stays
// nil rather than defaulting to a false the graph would treat as "observed
// unencrypted").
type BlockDevice struct {
	// DeviceName is the block-device mapping name (for example /dev/xvda).
	// Optional.
	DeviceName *string `json:"device_name,omitempty"`

	// VolumeID is the EBS volume id the block device maps to. Optional; the
	// KMS posture consumer keys on it when present.
	VolumeID *string `json:"volume_id,omitempty"`

	// DeleteOnTermination reports the delete-on-termination flag. Optional.
	DeleteOnTermination *bool `json:"delete_on_termination,omitempty"`

	// Status is the block-device attachment status string. Optional.
	Status *string `json:"status,omitempty"`

	// Encrypted reports the volume encryption flag. Optional pointer so nil
	// (unreported, e.g. DescribeInstances does not return per-volume
	// encryption) stays distinct from an observed false.
	Encrypted *bool `json:"encrypted,omitempty"`
}
