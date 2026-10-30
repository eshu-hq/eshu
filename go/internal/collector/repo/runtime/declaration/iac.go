// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// ExtractIACEvidence normalizes private parser values from Terraform,
// Terragrunt, and Helm without retaining their configuration bodies.
func ExtractIACEvidence(e shared.RuntimeEvidence) Extraction {
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
		var section section
		switch v.SourceKind {
		case sourceKindTerraformRequired:
			section = sectionTerraformRequired
		case sourceKindTerragruntConstraint:
			section = sectionTerragruntConstraint
		case sourceKindHelmKubeVersion:
			section = sectionHelmKubeVersion
		default:
			rejected++
			continue
		}
		if v.Line < 1 || !safeSpec(v.Value) || (v.Runtime != "terraform" && v.Runtime != "kubernetes") {
			rejected++
			continue
		}
		order := 0
		for _, prior := range out {
			if prior.line == v.Line && prior.runtime == v.Runtime {
				order++
			}
		}
		out = append(out, declaration{line: v.Line, order: order, runtime: v.Runtime, rawSpec: v.Value, section: section})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

// UnavailableIACExtraction records a parser miss without claiming absence.
func UnavailableIACExtraction(byteSize int) Extraction {
	return ExtractIACEvidence(shared.RuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedIACExtraction records an over-cap file without retaining its body.
func CappedIACExtraction(byteSize int) Extraction {
	return ExtractIACEvidence(shared.RuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}
