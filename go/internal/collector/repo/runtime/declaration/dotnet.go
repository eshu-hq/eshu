// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// ExtractDotNetEvidence normalizes private parser values into guarded runtime facts.
func ExtractDotNetEvidence(e shared.DotNetRuntimeEvidence) Extraction {
	if e.State == "over_cap" || e.ByteSize > MaxManifestBytesPerFile {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: e.ByteSize, ByteCap: MaxManifestBytesPerFile}}
	}
	if e.State == "unavailable" {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: e.ByteSize}}
	}
	if e.State == "over_tokens" {
		return Extraction{skip: Skip{Reason: SkipReasonTokenCap, ByteSize: e.ByteSize, TokenCount: 129}}
	}
	out := make([]declaration, 0, len(e.Values))
	rejected := e.Rejected
	lineOrders := make(map[int]int)
	for _, v := range e.Values {
		var sec section
		switch v.SourceKind {
		case sourceKindDotNetGlobalSDK:
			sec = sectionDotNetGlobalSDK
		case sourceKindDotNetTargetFramework:
			sec = sectionDotNetTargetFramework
		case sourceKindDotNetTargetFrameworks:
			sec = sectionDotNetTargetFrameworks
		default:
			rejected++
			continue
		}
		if v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, order: lineOrders[v.Line], runtime: "dotnet", rawSpec: v.Value, section: sec})
		lineOrders[v.Line]++
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableDotNetExtraction reports a parser miss to the collector.
func UnavailableDotNetExtraction(byteSize int) Extraction {
	return ExtractDotNetEvidence(shared.DotNetRuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedDotNetExtraction reports an over-cap file without retaining it.
func CappedDotNetExtraction(byteSize int) Extraction {
	return ExtractDotNetEvidence(shared.DotNetRuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}
