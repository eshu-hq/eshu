// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// ListOSPackageAdvisoryTargetsForPackagesQueryForTest exposes the narrowed
// OS-package query text to the external live tests, which EXPLAIN it.
func ListOSPackageAdvisoryTargetsForPackagesQueryForTest() string {
	return listOSPackageAdvisoryTargetsForPackagesQuery()
}
