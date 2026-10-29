// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"path"

	pythonparser "github.com/eshu-hq/eshu/go/internal/parser/python"
	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
)

// ExtractPythonEvidence normalizes guarded parser evidence into declarations.
// It retains no reference to a source body and rejects unknown source kinds.
func ExtractPythonEvidence(e pythondep.PythonRuntimeEvidence) Extraction {
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
		section, ok := pythonSection(value.SourceKind)
		if !ok || value.Line < 1 || !safeSpec(value.Value) {
			rejected++
			continue
		}
		order := 0
		for _, previous := range out {
			if previous.line == value.Line {
				order++
			}
		}
		out = append(out, declaration{line: value.Line, order: order, runtime: "python", rawSpec: value.Value, section: section})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: e.ByteSize, Rejected: rejected}
	}
	return extraction
}

func pythonSection(kind string) (section, bool) {
	switch kind {
	case sourceKindPyProjectRequires:
		return sectionPyProjectRequires, true
	case sourceKindPoetryPython:
		return sectionPoetryPython, true
	case sourceKindPipfileVersion:
		return sectionPipfileVersion, true
	case sourceKindPipfileFullVersion:
		return sectionPipfileFullVersion, true
	case sourceKindSetupCFGRequires:
		return sectionSetupCFGRequires, true
	case sourceKindSetupPYRequires:
		return sectionSetupPYRequires, true
	default:
		return sectionNone, false
	}
}

func extractPythonTOMLManifest(relativePath, body string) Extraction {
	if len(body) > MaxManifestBytesPerFile {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: len(body), ByteCap: MaxManifestBytesPerFile}}
	}
	evidence, err := pythondep.ExtractPythonRuntimeEvidence(path.Base(relativePath), []byte(body))
	if err != nil {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: len(body)}}
	}
	return ExtractPythonEvidence(evidence)
}

func extractPythonSetupCFG(body string) Extraction {
	evidence, err := pythondep.ExtractSetupCFGRuntimeEvidence([]byte(body))
	if err != nil {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: len(body)}}
	}
	return ExtractPythonEvidence(evidence)
}

func extractPythonSetupPY(body string) Extraction {
	if len(body) > MaxManifestBytesPerFile {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: len(body), ByteCap: MaxManifestBytesPerFile}}
	}
	evidence, err := pythonparser.ExtractSetupPYRuntimeEvidence([]byte(body))
	if err != nil {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: len(body)}}
	}
	return ExtractPythonEvidence(evidence)
}

// UnavailablePythonExtraction reports a parser miss that cannot imply absence.
func UnavailablePythonExtraction(byteSize int) Extraction {
	return ExtractPythonEvidence(pythondep.PythonRuntimeEvidence{State: "unavailable", ByteSize: byteSize})
}

// CappedPythonExtraction reports a Python manifest over the declaration cap.
func CappedPythonExtraction(byteSize int) Extraction {
	return ExtractPythonEvidence(pythondep.PythonRuntimeEvidence{State: "over_cap", ByteSize: byteSize})
}
