// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// Odu is one scenario-level Ifá conformance case at the fact-envelope seam.
type Odu struct {
	Name  string
	Work  *projector.ScopeGenerationWork
	Facts []facts.Envelope
}

// CatalogOdu pairs one cataloged Odù with a short human-facing detail for
// coverage reporting (why the fixture exists, what it proves).
type CatalogOdu struct {
	// Odu is the cataloged scenario.
	Odu Odu
	// Detail is a one-line human description of what the Odù proves.
	Detail string
}
