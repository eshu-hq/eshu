// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// NewDefaultRegistry constructs the canonical reducer catalog for the default
// domain definitions, wiring handlers for the domains implemented today and
// allowing additive registration of source-neutral domains when handlers are
// provided explicitly.
func NewDefaultRegistry(handlers DefaultHandlers) (Registry, error) {
	registry := NewRegistry()
	for _, def := range implementedDefaultDomainDefinitions(handlers) {
		if err := registry.Register(def); err != nil {
			return Registry{}, err
		}
	}

	return registry, nil
}

// NewDefaultRuntime builds a reducer runtime from the default domain catalog.
//
// This is the additive seam for reducer main wiring: callers can replace the
// manual DefaultDomainDefinitions registration loop with one constructor call
// while keeping the surrounding service, queue, and polling setup unchanged.
func NewDefaultRuntime(handlers DefaultHandlers) (*Runtime, error) {
	registry, err := NewDefaultRegistry(handlers)
	if err != nil {
		return nil, err
	}

	rt, err := NewRuntime(registry)
	if err != nil {
		return nil, err
	}
	rt.GenerationCheck = handlers.GenerationCheck
	return rt, nil
}
