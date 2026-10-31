// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	elixirparser "github.com/eshu-hq/eshu/go/internal/parser/elixir"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ExtractElixirEvidence normalizes parser-private mix.exs version evidence.
// The parser has already read the literal elixir: requirement once and paired
// it with its real source line; unknown kinds and unpositioned values are
// rejected, never emitted.
func ExtractElixirEvidence(e shared.RuntimeEvidence) Extraction {
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
		if v.SourceKind != sourceKindMixExsElixir || v.Runtime != "elixir" || v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, runtime: "elixir", rawSpec: v.Value, section: sectionMixExsElixir})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableElixirExtraction records a missing native parse for retry. A
// proven unchanged malformed file keeps its typed skip; a transient miss is
// retried, never read as absence.
func UnavailableElixirExtraction(byteSize int) Extraction {
	return ExtractElixirEvidence(shared.RuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedElixirExtraction records a bounded mix.exs manifest skip.
func CappedElixirExtraction(byteSize int) Extraction {
	return ExtractElixirEvidence(shared.RuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}

// extractMixExsManifest reads one mix.exs body through the same literal
// scanner the native Elixir parse uses.
func extractMixExsManifest(body string) Extraction {
	return ExtractElixirEvidence(elixirparser.ExtractMixRuntimeEvidence([]byte(body)))
}
