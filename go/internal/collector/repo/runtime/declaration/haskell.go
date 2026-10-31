// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	yamlparser "github.com/eshu-hq/eshu/go/internal/parser/yaml"
)

// ExtractHaskellEvidence normalizes parser-private Stack resolver evidence.
// Only the root resolver string survives; a value the credential guard
// rejects is a counted drop, never a fact.
func ExtractHaskellEvidence(e shared.RuntimeEvidence) Extraction {
	if e.ByteSize > MaxManifestBytesPerFile || e.State == "over_cap" {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: e.ByteSize, ByteCap: MaxManifestBytesPerFile}}
	}
	if e.State == "unavailable" {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: e.ByteSize}}
	}
	if e.State == "over_tokens" {
		return Extraction{skip: Skip{Reason: SkipReasonTokenCap, ByteSize: e.ByteSize, TokenCount: e.TokenCount}}
	}
	out := make([]declaration, 0, len(e.Values))
	rejected := e.Rejected
	for _, v := range e.Values {
		if v.SourceKind != sourceKindStackYamlResolver || v.Runtime != "haskell" || v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, runtime: "haskell", rawSpec: v.Value, section: sectionStackYamlResolver})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableHaskellExtraction records a missing native parse for retry.
func UnavailableHaskellExtraction(byteSize int) Extraction {
	return ExtractHaskellEvidence(shared.RuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedHaskellExtraction records a bounded stack.yaml skip.
func CappedHaskellExtraction(byteSize int) Extraction {
	return ExtractHaskellEvidence(shared.RuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}

func extractStackManifest(body string) Extraction {
	return ExtractHaskellEvidence(yamlparser.ExtractStackRuntimeEvidence([]byte(body)))
}

// ExtractHaskellCabalEvidence normalizes private Cabal tested-with values
// into guarded runtime facts, re-applying the credential guard at emission.
func ExtractHaskellCabalEvidence(e shared.RuntimeEvidence) Extraction {
	if e.ByteSize > MaxManifestBytesPerFile || e.State == "over_cap" {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: e.ByteSize, ByteCap: MaxManifestBytesPerFile}}
	}
	if e.State == "unavailable" {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: e.ByteSize}}
	}
	if e.State == "over_tokens" {
		return Extraction{skip: Skip{Reason: SkipReasonTokenCap, ByteSize: e.ByteSize, TokenCount: e.TokenCount}}
	}
	out := make([]declaration, 0, len(e.Values))
	rejected, lineOrders := e.Rejected, make(map[int]int)
	for _, v := range e.Values {
		if v.SourceKind != sourceKindCabalTestedWith || v.Runtime != "haskell" || v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, order: lineOrders[v.Line], runtime: "haskell", rawSpec: v.Value, section: sectionCabalTestedWith})
		lineOrders[v.Line]++
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}
