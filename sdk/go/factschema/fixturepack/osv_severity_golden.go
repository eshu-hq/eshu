// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import _ "embed"

//go:embed payloads/osv_severity_golden.json
var osvSeverityGolden []byte

// OSVSeverityGolden returns the shared synthetic OSV records and the exact
// severity fields a conforming OSV collector writes on the vulnerability.cve
// payload: cvss_vector from the CVSS_V4, then CVSS_V3, severity[] entry, and
// severity_label from database_specific.severity. The extracted collector and
// core both replay these records to catch severity drift.
func OSVSeverityGolden() []byte {
	return append([]byte(nil), osvSeverityGolden...)
}
