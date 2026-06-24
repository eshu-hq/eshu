// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewEC2InstancePostureEnvelope builds the durable ec2_instance_posture fact for
// one EC2 instance's derived security and operations posture. It requires the
// instance id (or ARN) as a join anchor and copies only IMDS settings,
// presence/derived booleans, the instance-profile ARN, and per-volume
// block-device metadata into the payload. The raw user-data string, console
// output, and any other instance payload never reach this builder.
//
// It emits no graph edges: the USES_PROFILE join to the IAM instance profile,
// the block-device KMS posture projection, and the derived internet-exposed
// flag are reducer-owned consumers (#1146, #1304, #1135).
func NewEC2InstancePostureEnvelope(observation EC2InstancePostureObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	arn := strings.TrimSpace(observation.ARN)
	instanceID := strings.TrimSpace(observation.InstanceID)
	if arn == "" && instanceID == "" {
		return facts.Envelope{}, fmt.Errorf("ec2 instance posture observation requires instance_id or arn")
	}
	identity := instanceID
	if identity == "" {
		identity = arn
	}
	stableKey := facts.StableID(facts.EC2InstancePostureFactKind, map[string]any{
		"account_id":  observation.Boundary.AccountID,
		"instance_id": identity,
		"region":      observation.Boundary.Region,
	})
	anchors := normalizedAnchors(nil, arn, instanceID)
	payload := map[string]any{
		"account_id":            observation.Boundary.AccountID,
		"region":                observation.Boundary.Region,
		"service_kind":          observation.Boundary.ServiceKind,
		"collector_instance_id": observation.Boundary.CollectorInstanceID,
		"resource_type":         ResourceTypeEC2Instance,
		"arn":                   arn,
		"instance_id":           instanceID,
		"state":                 strings.TrimSpace(observation.State),

		"imds_v2_required":            boolOrNil(observation.IMDSv2Required),
		"imds_http_endpoint":          strings.TrimSpace(observation.HTTPEndpoint),
		"imds_http_put_hop_limit":     int32OrNil(observation.HTTPPutResponseHopLimit),
		"user_data_present":           boolOrNil(observation.UserDataPresent),
		"detailed_monitoring_enabled": observation.DetailedMonitoring,
		"ebs_optimized":               observation.EBSOptimized,
		"public_ip_associated":        observation.PublicIPAssociated,
		"public_ip_address":           strings.TrimSpace(observation.PublicIPAddress),
		"instance_profile_arn":        strings.TrimSpace(observation.InstanceProfileARN),
		"tenancy":                     strings.TrimSpace(observation.Tenancy),
		"nitro_enclave_enabled":       observation.NitroEnclaveEnabled,
		"block_devices":               ec2BlockDeviceMaps(observation.BlockDevices),

		"correlation_anchors": anchors,
	}
	return newEnvelope(
		observation.Boundary,
		facts.EC2InstancePostureFactKind,
		facts.EC2InstancePostureSchemaVersionV1,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity+"#posture"),
		observation.SourceURI,
		payload,
	), nil
}

// ec2BlockDeviceMaps normalizes per-volume block-device metadata into stable
// payload maps. It returns nil for an empty input so posture payloads carry a
// stable shape. Encryption stays nil unless the observation already carries it;
// DescribeInstances does not report per-volume encryption.
func ec2BlockDeviceMaps(devices []EC2BlockDevicePosture) []map[string]any {
	if len(devices) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		output = append(output, map[string]any{
			"device_name":           strings.TrimSpace(device.DeviceName),
			"volume_id":             strings.TrimSpace(device.VolumeID),
			"delete_on_termination": device.DeleteOnTermination,
			"status":                strings.TrimSpace(device.Status),
			"encrypted":             boolOrNil(device.Encrypted),
		})
	}
	return output
}

// int32OrNil returns the dereferenced int32 or nil when input is nil, so an
// unreported hop limit stays distinct from an observed zero.
func int32OrNil(input *int32) any {
	if input == nil {
		return nil
	}
	return *input
}
