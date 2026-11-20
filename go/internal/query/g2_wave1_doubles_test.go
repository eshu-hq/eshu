// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Shared doubles for the G2 Wave-1 seven-leg suite. Each counting
// admission wraps a serving inner admission and records how many
// times each re-declared port method runs, so the auth (zero-contact)
// and parity (exactly-one-contact) legs prove which lane the bytes
// came through. They implement the v0 pins by construction
// (compile-asserted below), never the wider static packages.

// countingExtractionReadiness records Catalog/Lookup calls.
type countingExtractionReadiness struct {
	inner          ExtractionReadinessReadAdmission
	catalogCalls   int
	lookupCalls    int
	lookupFamilies []scope.CollectorKind
}

var _ ExtractionReadinessReadAdmission = (*countingExtractionReadiness)(nil)

func (c *countingExtractionReadiness) Catalog() []extraction.Readiness {
	c.catalogCalls++
	return c.inner.Catalog()
}

func (c *countingExtractionReadiness) Lookup(family scope.CollectorKind) (extraction.Readiness, bool) {
	c.lookupCalls++
	c.lookupFamilies = append(c.lookupFamilies, family)
	return c.inner.Lookup(family)
}

func (c *countingExtractionReadiness) contacts() int { return c.catalogCalls + c.lookupCalls }

// countingFactSchema records SupportedSchemaVersions/SchemaVersion/
// ClassifySchemaVersion calls.
type countingFactSchema struct {
	inner         FactSchemaReadAdmission
	registryCalls int
	versionCalls  int
	classifyCalls int
}

var _ FactSchemaReadAdmission = (*countingFactSchema)(nil)

func (c *countingFactSchema) SupportedSchemaVersions() map[string]string {
	c.registryCalls++
	return c.inner.SupportedSchemaVersions()
}

func (c *countingFactSchema) SchemaVersion(factKind string) (string, bool) {
	c.versionCalls++
	return c.inner.SchemaVersion(factKind)
}

func (c *countingFactSchema) ClassifySchemaVersion(factKind, candidate string) facts.Compatibility {
	c.classifyCalls++
	return c.inner.ClassifySchemaVersion(factKind, candidate)
}

func (c *countingFactSchema) contacts() int {
	return c.registryCalls + c.versionCalls + c.classifyCalls
}

// countingSurfaceInventory records LoadSurfaceInventory calls.
type countingSurfaceInventory struct {
	inner     SurfaceInventoryReadAdmission
	loadCalls int
}

var _ SurfaceInventoryReadAdmission = (*countingSurfaceInventory)(nil)

func (c *countingSurfaceInventory) LoadSurfaceInventory() (capabilitycatalog.SurfaceInventory, error) {
	c.loadCalls++
	return c.inner.LoadSurfaceInventory()
}
