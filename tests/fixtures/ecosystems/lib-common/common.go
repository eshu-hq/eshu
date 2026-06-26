// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lib is an IP-free fixture library published as github.com/acme/lib-common,
// consumed cross-repo by orders-api to exercise DEPENDS_ON (rc-3).
package lib

// Identity returns its argument unchanged. Minimal stand-in, no proprietary logic.
func Identity[T any](value T) T {
	return value
}
