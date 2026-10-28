// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package component adapts the three in-tree scanner analyzers
// (image_unpacking, sbom_generation, os_package_extraction) to the
// collector-sdk/v1alpha3 stdio contract so the core extension host can
// execute each analyzer as a separate component process with per-analyzer
// manifests and producer grants. Refs #8878, #9385.
package component
