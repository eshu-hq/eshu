// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observability emits observability facts from repository configuration
// files: log routes, metrics, trace routes, and the source instances they
// belong to.
//
// It reads the parsed file payloads the snapshot already produced rather than
// re-reading files from disk.
package observability
