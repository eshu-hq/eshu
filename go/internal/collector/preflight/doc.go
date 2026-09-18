// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package preflight groups collector-owned safety classifiers that inspect
// bundled documentation payloads before any extractor reads member content.
//
// This package is a documentation-only namespace. Preflight implementations
// live in its leaf packages; collection, extraction, fact emission, and
// runtime behavior stay in the owning collector packages.
package preflight
