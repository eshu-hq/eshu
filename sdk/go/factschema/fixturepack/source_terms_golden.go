// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import _ "embed"

//go:embed payloads/source_terms_golden.json
var sourceTermsGolden []byte

// SourceTermsGolden returns the shared per-source license and attribution a
// conforming collector writes on each vulnerability.source_snapshot payload
// (eshu-hq/eshu#9958). The sources array is sorted by source; each entry names
// the source token, its license (an SPDX identifier, a LicenseRef- identifier,
// or NOASSERTION) and one attribution line. A source absent from the file
// carries neither key. The extracted collector and core both replay it to catch
// per-source constant drift.
func SourceTermsGolden() []byte {
	return append([]byte(nil), sourceTermsGolden...)
}
