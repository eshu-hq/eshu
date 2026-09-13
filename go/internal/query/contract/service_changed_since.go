// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import "github.com/eshu-hq/eshu/go/internal/query/freshness"

// freshnessServiceChangedSinceCapability is the unexported root spelling this
// package's handlers read. It forwards
// freshness.ServiceChangedSinceCapability rather than repeating the string, so
// the compiler enforces what a test used to assert. See that const for what
// the route reads and which evidence families it reports.
const freshnessServiceChangedSinceCapability = freshness.ServiceChangedSinceCapability

// Declared by the freshness family (freshness/capabilities.go, #6060), not
// copied here. See the semanticsearch entry in contract_capability_matrix.go
// for why.
func init() {
	register(freshnessServiceChangedSinceCapability, freshness.ServiceChangedSinceSupport())
}
