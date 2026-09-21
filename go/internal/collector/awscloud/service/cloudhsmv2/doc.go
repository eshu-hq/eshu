// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudhsmv2 maps AWS CloudHSM v2 cluster and backup metadata into AWS
// cloud collector facts.
//
// The scanner emits reported-confidence resources for CloudHSM v2 clusters
// (carrying embedded HSM ENI placement metadata and certificate-presence flags)
// and backups, plus relationships for cluster-in-VPC, cluster-in-subnet,
// cluster-uses-security-group, and backup-of-cluster evidence. Cryptographic key
// material, certificate PEM bodies, the cluster certificate signing request
// body, and the cluster's Pre-Crypto Officer password stay outside this package
// contract: the scanner is metadata-only and records certificate presence as a
// boolean, never the body.
package cloudhsmv2
