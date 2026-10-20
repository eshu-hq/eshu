// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ds maps AWS Directory Service metadata into AWS cloud collector facts
// across AWS Managed Microsoft AD, Simple AD, and AD Connector directories.
//
// The package owns scanner-level normalization only. It never calls the AWS SDK
// directly, never calls a mutation API (ResetUserPassword,
// Create/Delete/Update/Enable/Disable/...), and never persists the directory
// admin password, the RADIUS shared secret, or the AD Connector service-account
// credentials. SDK adapters provide Directory, Trust, SharedDirectory, and
// LDAPSSetting values; Scanner emits aws_resource facts for directories, trust
// relationships, and shared directories plus directory-to-VPC,
// directory-to-subnet, trust-to-directory, shared-directory-to-owner-directory,
// and shared-directory-to-owner-account relationship evidence.
//
// Every relationship sets a non-empty target_type and a target_resource_id that
// matches the target scanner's resource_id: the directory resource_id is the
// bare directory ID (d-xxxxxxxxxx), so the FSx Active Directory join edges that
// target the bare directory ID resolve against it. VPC and subnet edges use the
// bare AWS ID (aws_ec2_vpc, aws_ec2_subnet); trust and shared-directory edges
// use the bare directory ID; and the owner-account edge uses the bare 12-digit
// account ID with no synthesized ARN.
package ds
