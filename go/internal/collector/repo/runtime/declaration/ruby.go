// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"

// ExtractRubyEvidence normalizes private Bundler parser evidence into runtime declarations.
func ExtractRubyEvidence(e rubyparser.RubyRuntimeEvidence) Extraction {
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
		var sec section
		switch v.SourceKind {
		case sourceKindGemfileRuby:
			sec = sectionGemfileRuby
		case sourceKindGemfileLockRuby:
			sec = sectionGemfileLockRuby
		default:
			rejected++
			continue
		}
		if v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, runtime: "ruby", rawSpec: v.Value, section: sec})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableRubyExtraction reports a native parser miss to the collector.
func UnavailableRubyExtraction(byteSize int) Extraction {
	return ExtractRubyEvidence(rubyparser.RubyRuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedRubyExtraction reports an over-cap Bundler file without retaining it.
func CappedRubyExtraction(byteSize int) Extraction {
	return ExtractRubyEvidence(rubyparser.RubyRuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}
