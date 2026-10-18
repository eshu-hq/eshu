// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ecr adapts Amazon ECR registry coordinates to the provider-neutral
// OCI registry contract.
//
// The package owns ECR registry URI construction and the seam where an AWS
// GetAuthorizationToken result becomes Distribution basic auth credentials. It
// keeps AWS profile, region, target registry host, and STS policy outside the
// fact model so callers can wire runtime-specific credential behavior.
//
// NewReferrerClient builds a Distribution client whose basic-auth credentials
// come from a fresh GetAuthorizationToken exchange, resolving the registry host
// from the supplied options or the token-exchange proxy endpoint. It is the ECR
// auth path for SBOM attestation oci_referrer fetches.
package ecr
