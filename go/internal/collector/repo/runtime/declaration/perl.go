// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// ExtractPerlEvidence normalizes private cpanfile parser evidence into
// guarded runtime declarations. Only the cpanfile_perl source kind survives,
// and every value passes the shared credential guard before emission.
func ExtractPerlEvidence(e shared.RuntimeEvidence) Extraction {
	if e.State == "over_cap" || e.ByteSize > MaxManifestBytesPerFile {
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
		if v.SourceKind != sourceKindCpanfilePerl {
			rejected++
			continue
		}
		if v.Line < 1 || !safeSpec(v.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: v.Line, runtime: "perl", rawSpec: v.Value, section: sectionCpanfilePerl})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}
