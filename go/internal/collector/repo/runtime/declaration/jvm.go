// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// ExtractJVMEvidence normalizes parser-private JVM values into version facts.
func ExtractJVMEvidence(e shared.RuntimeEvidence) Extraction {
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
		section, ok := jvmSection(value.SourceKind)
		if !ok || value.Line < 1 || !safeSpec(value.Value) || (value.Runtime != "java" && value.Runtime != "scala" && value.Runtime != "sbt") {
			rejected++
			continue
		}
		order := 0
		for _, previous := range out {
			if previous.line == value.Line && previous.runtime == value.Runtime {
				order++
			}
		}
		out = append(out, declaration{line: value.Line, order: order, runtime: value.Runtime, rawSpec: value.Value, section: section})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

func jvmSection(kind string) (section, bool) {
	switch kind {
	case sourceKindPomCompilerRelease:
		return sectionPomCompilerRelease, true
	case sourceKindPomCompilerSource:
		return sectionPomCompilerSource, true
	case sourceKindPomCompilerTarget:
		return sectionPomCompilerTarget, true
	case sourceKindPomJavaVersion:
		return sectionPomJavaVersion, true
	case sourceKindGradleToolchain:
		return sectionGradleToolchain, true
	case sourceKindGradleTargetCompatibility:
		return sectionGradleTargetCompatibility, true
	case sourceKindGradleCompatibility:
		return sectionGradleCompatibility, true
	case sourceKindGradleKotlinToolchain:
		return sectionGradleKotlinToolchain, true
	case sourceKindSbtScalaVersion:
		return sectionSbtScalaVersion, true
	case sourceKindSbtBuildVersion:
		return sectionSbtBuildVersion, true
	default:
		return sectionNone, false
	}
}

// UnavailableJVMExtraction records a parser miss without treating it as absence.
func UnavailableJVMExtraction(byteSize int) Extraction {
	return ExtractJVMEvidence(shared.RuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedJVMExtraction records a skipped, over-cap JVM source.
func CappedJVMExtraction(byteSize int) Extraction {
	return ExtractJVMEvidence(shared.RuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}
