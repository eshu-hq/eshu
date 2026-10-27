// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import _ "embed"

//go:embed payloads/nvd_paging_golden.json
var nvdPagingGolden []byte

// NVDPagingGolden returns the shared synthetic two-page NVD input and exact
// expected fact kinds, stable keys and payloads. The extracted collector and core can use these bytes
// to catch incomplete pagination and payload drift.
func NVDPagingGolden() []byte {
	return append([]byte(nil), nvdPagingGolden...)
}
