// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	swiftparser "github.com/eshu-hq/eshu/go/internal/parser/swift"
)

// ExtractSwiftEvidence normalizes parser-private Package.swift tools-version
// evidence into guarded runtime facts.
func ExtractSwiftEvidence(e shared.RuntimeEvidence) Extraction {
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
	for _, value := range e.Values {
		if value.SourceKind != sourceKindSwiftToolsVersion || value.Runtime != "swift" || value.Line < 1 || !safeSpec(value.Value) {
			rejected++
			continue
		}
		out = append(out, declaration{line: value.Line, runtime: "swift", rawSpec: value.Value, section: sectionSwiftToolsVersion})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableSwiftExtraction records a malformed Package.swift for retry: a
// marker without a value is malformed, never an absent declaration.
func UnavailableSwiftExtraction(byteSize int) Extraction {
	return ExtractSwiftEvidence(shared.RuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedSwiftExtraction records a bounded Package.swift skip.
func CappedSwiftExtraction(byteSize int) Extraction {
	return ExtractSwiftEvidence(shared.RuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}

func extractSwiftPackageManifest(body string) Extraction {
	return ExtractSwiftEvidence(swiftparser.ExtractSwiftToolsVersionEvidence([]byte(body)))
}
