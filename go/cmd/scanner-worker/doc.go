// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command scanner-worker runs isolated scanner-worker claims for CPU-heavy or
// memory-heavy security analyzers.
//
// The binary consumes workflow work items with collector_kind=scanner_worker,
// builds scannerworker.ClaimInput values with resource limits, commits source
// facts under the claim fence, and records bounded retry or dead-letter
// payloads. It can run concrete image_unpacking, sbom_generation
// repository-manifest, and os_package_extraction rootfs analyzers. Image
// unpacking emits coverage or unsupported evidence, but the binary does not
// emit reducer-owned findings.
package main
