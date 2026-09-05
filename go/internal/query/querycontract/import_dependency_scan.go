// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// ImportDependencyInternalScanLimit bounds the internal content-store scan
// behind an import-dependency read. Callers request one row past the bound
// (scan_limit = ImportDependencyInternalScanLimit + 1) so an overflow is
// observable as a 422 rather than a silently truncated page.
//
// This is the ONLY declaration of the bound. Root package query's P0 shim
// (family_code_shim.go) aliases it so the code-family dependency readers that
// have not moved yet keep compiling unchanged; the staying grant and
// queryplan tests name this package directly so the seeded edge counts stay
// pinned to the same bound the read enforces. A second literal in root would
// compile and drift silently, flipping either the enforced bound or the
// tests that prove it with nothing failing.
const ImportDependencyInternalScanLimit = 25000
