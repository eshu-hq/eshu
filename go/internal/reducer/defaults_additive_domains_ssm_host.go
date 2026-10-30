// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

// appendSSMHostAdditiveDomains registers the SSM host inventory observation
// materialization domain (eshu-hq/eshu#9558). The registration is gated on the
// fact loader, the host graph writer, AND valid freshness bounds: registering
// without bounds would project supersession under unbounded rules, and
// registering without the writer would silently drop every SSM observation
// before it reached the graph.
func appendSSMHostAdditiveDomains(definitions []DomainDefinition, handlers DefaultHandlers) []DomainDefinition {
	if handlers.FactLoader == nil || handlers.SSMHostGraphWriter == nil {
		return definitions
	}
	if err := handlers.SSMHostBounds.Validate(); err != nil {
		return definitions
	}
	definition := ssmhost.MaterializationDomainDefinition()
	definition.Handler = ssmhost.MaterializationHandler{
		FactLoader:  handlers.FactLoader,
		GraphWriter: handlers.SSMHostGraphWriter,
		Bounds:      handlers.SSMHostBounds,
		Instruments: handlers.Instruments,
	}
	return append(definitions, definition)
}
