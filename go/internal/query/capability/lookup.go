// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capability

import (
	// The capability rows live in query/contract and register themselves
	// through querycontract.RegisterCapabilities in their init()s. This blank
	// import is what links them into the binary; root never writes the
	// registry itself any more.
	_ "github.com/eshu-hq/eshu/go/internal/query/contract"
)

// CatalogKey is the capability id for reads of the embedded capability
// catalog. It lives here rather than in root's capability_keys.go because
// this package and root both name it; the rest of that file's keys are
// root's alone. Not CatalogCapability -- that would stutter against the
// package name (docs/internal/naming.md rule 4).
const CatalogKey = "capability_catalog.list"
