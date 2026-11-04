// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import _ "embed"

//go:embed payloads/osv_identity_edges_golden.json
var osvIdentityEdgesGolden []byte

// OSVIdentityEdgesGolden returns the shared synthetic OSV records and the exact
// identity-edge fields a conforming OSV collector writes on the
// vulnerability.cve payload: aliases, related and upstream (each trimmed,
// de-duplicated and sorted) and status "withdrawn" for a withdrawn record. The
// upstream edge is directed and never an alias. The extracted collector and
// core both replay these records to catch identity-edge drift.
func OSVIdentityEdgesGolden() []byte {
	return append([]byte(nil), osvIdentityEdgesGolden...)
}
