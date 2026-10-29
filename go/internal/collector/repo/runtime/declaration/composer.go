// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"

	parserjson "github.com/eshu-hq/eshu/go/internal/parser/json"
)

// ExtractComposerEvidence normalizes typed parser evidence into declarations.
// The parser has already validated the complete document and paired each
// last-wins string value with its real source line.
func ExtractComposerEvidence(e parserjson.ComposerRuntimeEvidence) Extraction {
	if e.ByteSize > MaxManifestBytesPerFile {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: e.ByteSize, ByteCap: MaxManifestBytesPerFile}}
	}
	if e.Unavailable {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: e.ByteSize}}
	}
	out := make([]declaration, 0, len(e.Values))
	rejected := e.Rejected
	for _, value := range e.Values {
		section, ok := composerSectionForKind(value.SourceKind)
		if !ok || value.Line < 1 {
			rejected++
			continue
		}
		raw := strings.TrimSpace(value.Value)
		if !safeSpec(raw) {
			rejected++
			continue
		}
		order := 0
		for _, d := range out {
			if d.line == value.Line {
				order++
			}
		}
		out = append(out, declaration{line: value.Line, order: order, runtime: "php", rawSpec: raw, section: section})
	}
	extraction := Extraction{declarations: detach(out)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, Rejected: rejected, ByteSize: e.ByteSize}
	}
	return extraction
}

func composerSectionForKind(kind string) (section, bool) {
	switch kind {
	case sourceKindComposerRequire:
		return sectionComposerRequire, true
	case sourceKindComposerPlatform:
		return sectionComposerPlatform, true
	case sourceKindComposerLock:
		return sectionComposerLock, true
	case sourceKindComposerOverrides:
		return sectionComposerOverrides, true
	default:
		return sectionNone, false
	}
}

func extractComposer(body string, lock bool) ([]declaration, int, error) {
	filename := "composer.json"
	if lock {
		filename = "composer.lock"
	}
	evidence, err := parserjson.ExtractComposerRuntimeEvidence(filename, []byte(body))
	if err != nil {
		return nil, 0, err
	}
	extraction := ExtractComposerEvidence(evidence)
	if extraction.skip.Reason == SkipReasonManifestUnparseable {
		return nil, 0, errComposerEvidenceUnavailable{}
	}
	return extraction.declarations, extraction.skip.Rejected, nil
}

type errComposerEvidenceUnavailable struct{}

func (errComposerEvidenceUnavailable) Error() string { return "composer evidence unavailable" }

// UnavailableComposerExtraction reports a Composer file whose native parser
// did not produce trustworthy evidence. The collector may retry a transient
// miss, while a proven unchanged malformed file can keep its typed skip.
func UnavailableComposerExtraction(byteSize int) Extraction {
	return ExtractComposerEvidence(parserjson.ComposerRuntimeEvidence{Unavailable: true, ByteSize: byteSize})
}

// CappedComposerExtraction reports a Composer document proven over the
// manifest byte cap without retaining or parsing its body.
func CappedComposerExtraction(byteSize int) Extraction {
	return ExtractComposerEvidence(parserjson.ComposerRuntimeEvidence{ByteSize: byteSize})
}
