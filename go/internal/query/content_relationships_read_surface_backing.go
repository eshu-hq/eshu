// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// ReadSurfaceGoSymbolBackings is the closed set of unexported query-package
// symbols that back a specs/language-feature-parity-ledger.v1.yaml
// read_surfaces label whose real consumer is a Go symbol rather than an MCP
// tool or HTTP route -- currently just "content_relationships" ->
// buildContentRelationshipSet (go/internal/mcp's #5335 read-surface
// consumer-existence gate resolves the label against this map's keys). The
// ref format is "<file>.go:<symbol>", matching the go_symbol backing kind in
// go/internal/mcp/read_surface_consumer_existence.go. Exported (rather than
// test-only) so the #5335 gate, which lives in package mcp to reach
// ReadOnlyTools(), can import it without a query -> mcp import cycle.
var ReadSurfaceGoSymbolBackings = map[string]bool{
	"content_relationships.go:buildContentRelationshipSet": true,
}

// The blank assignment below references buildContentRelationshipSet
// directly. If that symbol is renamed or removed, this line fails to compile
// -- which fails `go build` (not just `go test`) package-wide -- keeping the
// go_symbol backing honest without a runtime reflection check. This is the
// "a test that references the symbol so a rename breaks compilation" proof
// #5335's design calls for, made stronger by living in the production build
// instead of only the test binary.
var _ = buildContentRelationshipSet
