// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

import (
	"fmt"
	"io"
	"sort"
)

// Exporter renders one [Snapshot] into one wire [Format].
//
// Implementations must be deterministic, must drop findings and components
// whose own scope identifiers disagree with snapshot.Scope, and must apply
// opts.Redactor (when non-nil) to manifest paths and locator URIs before
// serialization.
type Exporter interface {
	Format() Format
	Export(w io.Writer, snapshot Snapshot, opts Options) error
}

// Registry resolves a [Format] to a registered [Exporter].
//
// The zero value is unusable; build a registry with [NewRegistry] which
// pre-registers every exporter shipped today.
type Registry struct {
	exporters map[Format]Exporter
}

// NewRegistry returns the default registry with every shipped exporter
// pre-registered.
//
// SARIF ships today. CycloneDX BOV, SPDX, and the GitHub dependency snapshot
// format are reserved formats; they are not registered until their exporter
// implementations land, so [Registry.Export] returns [ErrUnsupportedFormat]
// for those values.
func NewRegistry() *Registry {
	registry := &Registry{exporters: make(map[Format]Exporter)}
	registry.Register(NewSARIFExporter())
	return registry
}

// Register installs an exporter for one format.
//
// Registering twice for the same format panics so a double-registration is
// caught at startup rather than silently overwriting one wire contract with
// another.
func (r *Registry) Register(exporter Exporter) {
	if exporter == nil {
		panic("exports: nil exporter")
	}
	format := exporter.Format()
	if format == "" {
		panic("exports: exporter returned empty format")
	}
	if _, exists := r.exporters[format]; exists {
		panic(fmt.Sprintf("exports: format %q already registered", format))
	}
	r.exporters[format] = exporter
}

// Export renders snapshot as format and writes the bytes to w.
//
// Validation happens in this order: format must be registered, scope must
// pass [Scope.Validate], then the exporter receives the snapshot. Exporters
// re-apply their own validation as defense in depth.
func (r *Registry) Export(w io.Writer, format Format, snapshot Snapshot, opts Options) error {
	exporter, ok := r.exporters[format]
	if !ok {
		return fmt.Errorf("export %s: %w", format, ErrUnsupportedFormat)
	}
	if err := snapshot.Scope.Validate(); err != nil {
		return fmt.Errorf("export %s: %w", format, err)
	}
	return exporter.Export(w, snapshot, opts)
}

// SupportedFormats returns the registered formats in stable wire-string
// order.
func (r *Registry) SupportedFormats() []Format {
	out := make([]Format, 0, len(r.exporters))
	for format := range r.exporters {
		out = append(out, format)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ErrUnsupportedFormat is returned by [Registry.Export] when the format has
// no registered exporter.
var ErrUnsupportedFormat = fmt.Errorf("unsupported export format")
