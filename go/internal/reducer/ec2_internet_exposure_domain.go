// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

const (
	// DomainEC2InternetExposureMaterialization derives conservative EC2
	// internet-exposure state from ec2_instance_posture public-IP evidence plus
	// ENI/security-group/rule facts and writes reducer-owned properties onto
	// existing EC2 CloudResource nodes. It is node-property-only on the
	// cloud_resource_uid keyspace and never persists raw public IP addresses.
	DomainEC2InternetExposureMaterialization Domain = "ec2_internet_exposure_materialization"
)
