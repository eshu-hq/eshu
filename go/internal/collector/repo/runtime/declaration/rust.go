// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	rustparser "github.com/eshu-hq/eshu/go/internal/parser/rust"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ExtractRustEvidence normalizes parser-private Cargo version evidence.
func ExtractRustEvidence(e shared.RuntimeEvidence) Extraction {
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
		if v.SourceKind != sourceKindCargoRustVersion || v.Runtime != "rust" || v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, runtime: "rust", rawSpec: v.Value, section: sectionCargoRustVersion})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableRustExtraction records a missing native parse for retry.
func UnavailableRustExtraction(byteSize int) Extraction {
	return ExtractRustEvidence(shared.RuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedRustExtraction records a bounded Cargo manifest skip.
func CappedRustExtraction(byteSize int) Extraction {
	return ExtractRustEvidence(shared.RuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}

func extractCargoManifest(body string) Extraction {
	return ExtractRustEvidence(rustparser.ExtractCargoRuntimeEvidence([]byte(body)))
}
