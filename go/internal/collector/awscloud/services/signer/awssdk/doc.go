// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Signer client into the
// metadata-only Signer scanner interface.
//
// The adapter uses ListSigningProfiles, GetSigningProfile, and
// ListSigningPlatforms to read Signer signing-profile and signing-platform
// control-plane metadata. It intentionally excludes StartSigningJob, SignPayload,
// ListSigningJobs, DescribeSigningJob, GetRevocationStatus, every profile
// permission API, and all Put/Cancel/Revoke/Add/Remove/Tag mutation APIs, so the
// adapter cannot start a signing operation, read signing material private keys,
// read signed-object payloads, or mutate Signer state.
package awssdk
