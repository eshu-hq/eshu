// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

// nvdWindowScopeVector is one shared-golden scope parse/format cell. The
// table, the canonical form, and the pinned digest must match
// eshu-hq/eshu-collector-vulnerability-intelligence, which resolves the
// scopes this package mints; a deliberate change updates both trees
// together (eshu-hq/eshu#9877 Path A).
type nvdWindowScopeVector struct {
	scopeID     string
	startMicros int64
	endMicros   int64
}

func nvdWindowScopeVectors() []nvdWindowScopeVector {
	return []nvdWindowScopeVector{
		{scopeID: "vuln-intel://nvd/_window/1767225600000000-1767398400000000", startMicros: 1767225600000000, endMicros: 1767398400000000},
		{scopeID: "vuln-intel://nvd/_window/1767398400000000-1767484800000000", startMicros: 1767398400000000, endMicros: 1767484800000000},
		{scopeID: "vuln-intel://nvd/_window/1000-2000", startMicros: 1000, endMicros: 2000},
	}
}

// canonicalNVDWindowScopeVectors renders the shared table in its canonical
// form for the cross-tree digest pin. The construction is byte-identical
// with the satellite tree's copy by contract.
func canonicalNVDWindowScopeVectors() string {
	var rendered string
	for _, vector := range nvdWindowScopeVectors() {
		rendered += fmt.Sprintf("%s|%d|%d\n", vector.scopeID, vector.startMicros, vector.endMicros)
	}
	digest := sha256.Sum256([]byte(rendered))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// nvdWindowScopeVectorsDigest pins the shared scope table. Update the
// vectors and the pin in both trees together.
const nvdWindowScopeVectorsDigest = "sha256:19e9b4ee8c5459fc524a83c435340f3f93bd0ee225414d98d0979da46b388eb6"

// TestNVDWindowScopeSharedGolden pins the scope parse/format contract the
// satellite resolves: the canonical vectors and digest match the satellite
// tree, so a minted scope parses to the same bounds on both sides.
func TestNVDWindowScopeSharedGolden(t *testing.T) {
	t.Parallel()

	if got := canonicalNVDWindowScopeVectors(); got != nvdWindowScopeVectorsDigest {
		t.Fatalf("window-scope shared-golden digest = %s, want %s (update the vectors and the pin in both trees together)",
			got, nvdWindowScopeVectorsDigest)
	}
	for _, vector := range nvdWindowScopeVectors() {
		start, end, ok := ParseNVDWindowScope(vector.scopeID)
		if !ok {
			t.Fatalf("ParseNVDWindowScope(%q) = not ok, want (%d, %d)", vector.scopeID, vector.startMicros, vector.endMicros)
		}
		if start.UnixMicro() != vector.startMicros || end.UnixMicro() != vector.endMicros {
			t.Fatalf("ParseNVDWindowScope(%q) = (%d, %d), want (%d, %d)",
				vector.scopeID, start.UnixMicro(), end.UnixMicro(), vector.startMicros, vector.endMicros)
		}
		if got := NVDWindowScopeID(start, end); got != vector.scopeID {
			t.Fatalf("NVDWindowScopeID(%d, %d) = %q, want %q",
				vector.startMicros, vector.endMicros, got, vector.scopeID)
		}
	}
}
