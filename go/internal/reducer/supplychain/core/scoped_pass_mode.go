// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Pass-mode counter labels: how one findings pass ran (#9779). The set is
// closed so the mode never explodes the series.
const (
	// passModeFull ran the whole finding set: no stamp, an uncapped stamp,
	// a genuine pass, or a scope that resolved to nothing seedable.
	passModeFull = "full"
	// passModeScoped re-derived the stamped CVEs only.
	passModeScoped = "scoped"
	// passModeScopedFallbackSentinel would have scoped but the stamp names
	// an unbounded change, so it ran full.
	passModeScopedFallbackSentinel = "scoped_fallback_sentinel"
)

// scopedWriteCVEIDs echoes the load's scope for the writer: the stamped CVEs
// when the evidence load scoped, empty for a full pass.
func scopedWriteCVEIDs(loaded supplyChainImpactLoadedEvidence) []string {
	if loaded.scoped == nil {
		return nil
	}
	return append([]string(nil), loaded.scoped.cves...)
}

// supplyChainImpactPassMode names one pass's mode for the pass-total
// counter: scoped when the evidence load scoped, sentinel-fallback when a
// one-hop pass carried the unbounded marker, full otherwise.
func supplyChainImpactPassMode(intent reducercontract.Intent, loaded supplyChainImpactLoadedEvidence) string {
	if loaded.scoped != nil {
		return passModeScoped
	}
	if fanOutOneHop(intent) && fanOutStampSentinel(fanOutStampCVEIDs(intent)) {
		return passModeScopedFallbackSentinel
	}
	return passModeFull
}
