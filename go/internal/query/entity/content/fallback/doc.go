// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fallback holds the entity content-fallback
// relationship build (Issue #9669, lane C2): the per-language and
// per-manifest-kind extraction the entity context fallback and the code
// relationships route serve when the graph has no edge.
//
// It moved here from the query root (content_relationships*.go) so a
// satellite consumer can satisfy the fallback leg without importing the
// query root: every extraction classifier reads only through the narrow
// querycontract.ContentRelationshipReader port, far smaller than the full
// ContentStore surface the root handlers need. Bodies are unchanged modulo
// package qualifiers; the query root keeps a thin forwarder type and a
// buildContentRelationshipSet adapter so its staying regression tests and
// the #5335 go_symbol backing keep compiling unchanged.
package fallback
