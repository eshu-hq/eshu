// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// EC2InstancePostureFactKind identifies one derived security/operations
	// posture observation for an EC2 instance. It is metadata-only control-plane
	// evidence read from the existing DescribeInstances pass: IMDS settings
	// (whether IMDSv2 is required, the hop limit, and the endpoint state),
	// user-data PRESENCE as a boolean only, detailed-monitoring and
	// EBS-optimized flags, public-IP association, the attached instance-profile
	// ARN, per-volume block-device metadata, and tenancy / Nitro-enclave state.
	//
	// It NEVER carries the user-data content (which can embed secrets), instance
	// console output, environment variables, command-line arguments, or any
	// other instance payload. The fact is source evidence only; reducers own the
	// USES_PROFILE join to the IAM instance profile (#1146), the block-device KMS
	// posture projection (#1304), and the derived internet-exposed flag (#1135).
	EC2InstancePostureFactKind = "ec2_instance_posture"

	// EC2InstancePostureSchemaVersionV1 is the first EC2 instance posture fact
	// schema.
	EC2InstancePostureSchemaVersionV1 = "1.0.0"
)

var ec2InstancePostureFactKinds = []string{
	EC2InstancePostureFactKind,
}

var ec2InstancePostureSchemaVersions = map[string]string{
	EC2InstancePostureFactKind: EC2InstancePostureSchemaVersionV1,
}

// EC2InstancePostureFactKinds returns the accepted EC2 instance posture fact
// kinds in source-contract order. The returned slice is a copy; mutating it does
// not change the registry.
func EC2InstancePostureFactKinds() []string {
	return slices.Clone(ec2InstancePostureFactKinds)
}

// EC2InstancePostureSchemaVersion returns the schema version for an EC2 instance
// posture fact kind, and reports whether the kind is registered.
func EC2InstancePostureSchemaVersion(factKind string) (string, bool) {
	version, ok := ec2InstancePostureSchemaVersions[factKind]
	return version, ok
}
