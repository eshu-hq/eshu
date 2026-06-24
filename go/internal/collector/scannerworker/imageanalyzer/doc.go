// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package imageanalyzer extracts installed package component evidence from
// configured local image rootfs or OCI layer tar inputs.
//
// The package implements the scanner-worker `image_unpacking` analyzer
// boundary. It reads only bounded local evidence configured by the runtime,
// preserves image identity and package database provenance on emitted source
// facts, records scanner_worker.analysis coverage for supported image targets,
// and records explicit unsupported warnings when package proof, image digest
// identity, or supported analyzer capability is missing. Registry access,
// advisory matching, vulnerability finding admission, and graph truth stay
// with their owning packages.
package imageanalyzer
