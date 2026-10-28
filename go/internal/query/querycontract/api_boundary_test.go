// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"testing"
)

// TestCoreReadContractsVersioned pins the #8872 versioned core read-contract
// set consumed by the API surface (GraphQuery, ContentStore, and the
// Postgres ReadStore reader surface named on CoreReadContractsVersion). A
// breaking change to any of those ports must bump the version in the same
// diff so the eshu-api extraction boundary stays explicit.
func TestCoreReadContractsVersioned(t *testing.T) {
	t.Parallel()

	if CoreReadContractsVersion != "v0" {
		t.Errorf("CoreReadContractsVersion = %q, want %q: bump it deliberately with the port change", CoreReadContractsVersion, "v0")
	}
}
