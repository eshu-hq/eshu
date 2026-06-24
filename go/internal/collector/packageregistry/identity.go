// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistry

import "github.com/eshu-hq/eshu/go/internal/packageidentity"

// NormalizePackageIdentity applies ecosystem-specific package identity rules
// before facts are assigned stable keys.
func NormalizePackageIdentity(identity PackageIdentity) (NormalizedPackageIdentity, error) {
	return packageidentity.Normalize(identity)
}

func normalizeRegistry(raw string) string {
	return packageidentity.NormalizeRegistry(raw)
}
