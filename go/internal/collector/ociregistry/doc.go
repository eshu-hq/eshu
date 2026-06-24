// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ociregistry normalizes OCI container registry evidence before it
// enters the durable fact envelope.
//
// The package belongs to the collector boundary for the oci_registry collector
// family. It owns repository and digest identity normalization plus
// reported-confidence envelope builders for repositories, mutable tag
// observations, manifests, image indexes, descriptors, referrers, and warnings.
// Manifest facts may carry redacted image config provenance labels when the
// runtime can fetch the config blob within its bounded scan limits. Builders
// validate boundary fields, keep tag evidence separate from digest identity,
// make FactID generation-specific, and redact unknown annotations, labels, or
// credential-bearing URLs.
//
// Provider adapters for Docker Hub, GHCR, ECR, JFrog, Harbor, Google Artifact
// Registry, and Azure Container Registry live in subpackages. The ociruntime
// package calls those clients to produce collected generations. This root
// package does not call live registries, materialize graph truth, or answer
// queries.
package ociregistry
