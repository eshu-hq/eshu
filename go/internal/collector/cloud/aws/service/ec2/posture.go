// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// instancePostureEnvelopes builds the metadata-only ec2_instance_posture fact
// for one instance. It reuses the instance record already fetched for the
// posture pass, so it adds no AWS API call, and emits no graph edges.
func instancePostureEnvelopes(boundary aws.Boundary, instance Instance) ([]facts.Envelope, error) {
	posture, err := aws.NewEC2InstancePostureEnvelope(instancePostureObservation(boundary, instance))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{posture}, nil
}

// instancePostureObservation maps the scanner-owned instance into the normalized
// ec2_instance_posture observation. Every value comes from already-reported
// DescribeInstances metadata; the scanner never reads user-data content,
// console output, or any other instance payload to build it.
func instancePostureObservation(
	boundary aws.Boundary,
	instance Instance,
) aws.EC2InstancePostureObservation {
	return aws.EC2InstancePostureObservation{
		Boundary:                boundary,
		ARN:                     strings.TrimSpace(instance.ARN),
		InstanceID:              strings.TrimSpace(instance.ID),
		State:                   strings.TrimSpace(instance.State),
		IMDSv2Required:          instance.IMDSv2Required,
		HTTPEndpoint:            strings.TrimSpace(instance.HTTPEndpoint),
		HTTPPutResponseHopLimit: instance.HTTPPutResponseHopLimit,
		UserDataPresent:         instance.UserDataPresent,
		DetailedMonitoring:      instance.DetailedMonitoring,
		EBSOptimized:            instance.EBSOptimized,
		PublicIPAssociated:      instance.PublicIPAssociated,
		PublicIPAddress:         strings.TrimSpace(instance.PublicIPAddress),
		InstanceProfileARN:      strings.TrimSpace(instance.InstanceProfileARN),
		Tenancy:                 strings.TrimSpace(instance.Tenancy),
		NitroEnclaveEnabled:     instance.NitroEnclaveEnabled,
		BlockDevices:            blockDevicePostures(instance.BlockDevices),
		SourceRecordID:          firstNonEmpty(strings.TrimSpace(instance.ID), strings.TrimSpace(instance.ARN)),
	}
}

// blockDevicePostures maps scanner-owned block devices into the aws posture
// shape. It returns nil for an empty input so the posture observation carries a
// stable slice.
func blockDevicePostures(devices []BlockDevice) []aws.EC2BlockDevicePosture {
	if len(devices) == 0 {
		return nil
	}
	output := make([]aws.EC2BlockDevicePosture, 0, len(devices))
	for _, device := range devices {
		output = append(output, aws.EC2BlockDevicePosture{
			DeviceName:          strings.TrimSpace(device.DeviceName),
			VolumeID:            strings.TrimSpace(device.VolumeID),
			DeleteOnTermination: device.DeleteOnTermination,
			Status:              strings.TrimSpace(device.Status),
			Encrypted:           device.Encrypted,
		})
	}
	return output
}
