// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the staying registration for the hardcoded-secret
// investigation capability. The route itself lives in the code-family file
// code_security_secrets.go, which moves out in a later #6060 phase and can no
// longer write root's package-private capabilityMatrix once it leaves this
// package -- so the registration lives here, in a staying file, against the
// leaf-owned entry (querycontract.HardcodedSecretCapability and
// querycontract.HardcodedSecretSupport, the single source for both).
//
// Init-order is safe by construction: Go initializes the imported
// querycontract package fully before running any init() in this package, and
// HardcodedSecretSupport() is a pure function with no package-variable
// dependencies, so this merge depends on no initialization order beyond what
// every other contract_*.go init() already relies on.
func init() {
	capabilityMatrix[querycontract.HardcodedSecretCapability] = querycontract.HardcodedSecretSupport()
}
