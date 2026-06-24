// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeartifact scans AWS CodeArtifact source truth into AWS cloud fact
// observations.
//
// The package owns scanner-level CodeArtifact fact selection for
// package-registry domains and repositories. It emits aws_resource facts for each
// domain and repository and aws_relationship facts for repository-to-domain
// membership, domain-to-KMS-key encryption, repository-to-upstream-repository
// routing, and repository-to-external-connection (public registry) links. SDK
// adapters belong in the awssdk subpackage so scanner tests can use small fakes
// instead of AWS SDK clients.
//
// The scanner is metadata-only. It never reads, downloads, publishes, copies,
// or deletes a package version or package asset; only domain and repository
// metadata plus external-connection and upstream-repository identifiers survive.
package codeartifact
