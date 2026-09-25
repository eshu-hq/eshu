// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package licensemanager maps AWS License Manager license-configuration
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for License Manager license
// configurations plus the configuration-applies-to-instance relationship for
// each EC2 instance association whose resource ARN resolves to a bare instance
// id. License entitlement tokens, checkout records, license usage measurements,
// and every grant, checkout, or mutation API stay outside this package
// contract: the scanner is metadata-only. EC2 host, EC2 AMI, RDS, and
// SSM-managed-instance associations are recorded as configuration metadata but
// are not keyed as graph edges, because Eshu does not yet publish a node those
// associations could join.
package licensemanager
