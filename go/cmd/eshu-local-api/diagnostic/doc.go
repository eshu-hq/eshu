// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package diagnostic captures bounded, private timing evidence for the API's
// repository ingester status route when explicitly enabled. It retains only
// allowlisted spans and closed labels and writes a mode 0600 report after the
// API server has finished its requests.
package diagnostic
