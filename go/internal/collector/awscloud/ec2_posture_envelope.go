// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
	payload, err := factschema.EncodeEC2InstancePosture(awsv1.EC2InstancePosture{
		AccountID:                 observation.Boundary.AccountID,
		Region:                    observation.Boundary.Region,
		ServiceKind:               boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID:       boundaryValue(observation.Boundary.CollectorInstanceID),
		ResourceType:              stringValuePtr(ResourceTypeEC2Instance),
		ARN:                       stringValuePtr(arn),
		InstanceID:                stringValuePtr(instanceID),
		State:                     stringValuePtr(strings.TrimSpace(observation.State)),
		IMDSv2Required:            observation.IMDSv2Required,
		IMDSHTTPEndpoint:          stringValuePtr(strings.TrimSpace(observation.HTTPEndpoint)),
		IMDSHTTPPutHopLimit:       observation.HTTPPutResponseHopLimit,
		UserDataPresent:           observation.UserDataPresent,
		DetailedMonitoringEnabled: boolValuePtr(observation.DetailedMonitoring),
		EBSOptimized:              boolValuePtr(observation.EBSOptimized),
		PublicIPAssociated:        boolValuePtr(observation.PublicIPAssociated),
		PublicIPAddress:           stringValuePtr(strings.TrimSpace(observation.PublicIPAddress)),
		InstanceProfileARN:        stringValuePtr(strings.TrimSpace(observation.InstanceProfileARN)),
		Tenancy:                   stringValuePtr(strings.TrimSpace(observation.Tenancy)),
		NitroEnclaveEnabled:       boolValuePtr(observation.NitroEnclaveEnabled),
		BlockDevices:              ec2BlockDevices(observation.BlockDevices),
		CorrelationAnchors:        anchors,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode ec2_instance_posture payload: %w", err)
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

func ec2BlockDevices(devices []EC2BlockDevicePosture) []awsv1.BlockDevice {
	if len(devices) == 0 {
		return nil
	}
	output := make([]awsv1.BlockDevice, 0, len(devices))
	for _, device := range devices {
		output = append(output, awsv1.BlockDevice{
			DeviceName:          stringValuePtr(strings.TrimSpace(device.DeviceName)),
			VolumeID:            stringValuePtr(strings.TrimSpace(device.VolumeID)),
			DeleteOnTermination: boolValuePtr(device.DeleteOnTermination),
			Status:              stringValuePtr(strings.TrimSpace(device.Status)),
			Encrypted:           device.Encrypted,
		})
	}
	return output
}
