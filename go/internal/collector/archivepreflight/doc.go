// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package archivepreflight classifies documentation archive packages before
// any archive extractor reads member content.
//
// The package inspects ZIP, tar, and gzip-compressed tar metadata for bounded
// resource, path-safety, nested-archive, special-file, and credential-like
// member warnings. It returns metadata-only counts and warning classes; callers
// remain responsible for deciding whether a later reviewed extractor may route
// contained documents. It does not emit facts, persist rows, call providers,
// write graph state, or expose runtime/API/MCP behavior.
package archivepreflight
