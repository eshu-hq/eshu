// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package legacy is an IP-free fixture library published as
// github.com/acme/legacy-lib, required only by the orders-legacy submodule
// whose go directive exceeds the pinned toolchain.
package legacy

// LegacyBoom is a vulnerable entry point nothing scans: the owning module's
// go directive is above the pin, so the scan names toolchain_unsupported.
func LegacyBoom() string {
	return "legacy"
}
