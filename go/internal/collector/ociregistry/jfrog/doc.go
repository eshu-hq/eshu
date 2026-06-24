// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package jfrog adapts JFrog Artifactory Docker/OCI repository settings to
// the provider-neutral OCI Distribution client.
//
// The package owns Artifactory URL construction and credential mapping only.
// It does not perform workflow claims, graph writes, or package-manager feed
// collection; npm, Maven, PyPI, NuGet, Go, and Generic Artifactory feeds belong
// to the package-registry collector lane.
package jfrog
