// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 CodeArtifact client into the
// metadata-only CodeArtifact scanner interface.
//
// The adapter uses ListDomains, DescribeDomain, ListRepositories, and
// DescribeRepository only. It intentionally excludes GetPackageVersionAsset,
// GetPackageVersionReadme, ListPackages, ListPackageVersions,
// ListPackageVersionAssets, ListPackageVersionDependencies,
// PublishPackageVersion, CopyPackageVersions, DisposePackageVersions, and every
// Create/Update/Delete/Put/Associate mutation API, so package payloads and
// resource mutation are unreachable by construction. A reflection guard test
// enforces the read-only, payload-free surface.
package awssdk
