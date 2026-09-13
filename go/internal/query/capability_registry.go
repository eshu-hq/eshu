// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	// The capability rows live in query/contract and register themselves
	// through querycontract.RegisterCapabilities in their init()s. This blank
	// import is what links them into the binary; root never writes the
	// registry itself any more.
	_ "github.com/eshu-hq/eshu/go/internal/query/contract"
)

// capabilityMatrix is the live registry, the same map query/contract
// registers into. Root reads it; it no longer owns it.
var capabilityMatrix = querycontract.CompatibilityCapabilityMatrix()
