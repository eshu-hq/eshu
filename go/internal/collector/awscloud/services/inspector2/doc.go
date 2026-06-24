// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package inspector2 maps Amazon Inspector v2 metadata into AWS cloud
// collector facts.
//
// The package owns scanner-level fact selection for account scan status,
// enabled scan features (EC2, ECR, Lambda, Lambda code), member accounts,
// findings filter non-criteria identity (ARN, name, action, owner ID), and CIS
// scan configuration metadata. It emits reported evidence only. Finding details
// (CVE, package version, affected host ARN), filter criteria expressions,
// descriptions, and reasons stay outside the scanner contract because they
// reveal exploitation surface or threat-hunting hypotheses.
package inspector2
