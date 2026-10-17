// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package distribution contains the OCI Distribution HTTP client used by
// provider adapters.
//
// The package implements bounded calls for the registry API surface Eshu needs
// before graph promotion: slash-preserving ping/challenge validation,
// repository-scoped token requests, bounded Link-aware tag listing with
// explicit completeness, manifest or index retrieval, and referrer listing
// with repository paths escaped one segment at a time. Tag continuations stay
// on the original registry origin and exact repository path; unsafe, cyclic,
// non-progressing, or oversized pagination returns bounded incomplete truth.
// The package does not know about ECR, JFrog, Docker Hub, GHCR, or any
// provider-specific repository discovery contract; provider packages build
// base URLs and credentials, then delegate OCI wire calls here.
package distribution
