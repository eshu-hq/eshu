// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"path"
	"strings"

	gradleparser "github.com/eshu-hq/eshu/go/internal/parser/gradle"
	parserjson "github.com/eshu-hq/eshu/go/internal/parser/json"
	mavenparser "github.com/eshu-hq/eshu/go/internal/parser/maven"
	"github.com/eshu-hq/eshu/go/internal/parser/nuget"
	rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"
	sbtparser "github.com/eshu-hq/eshu/go/internal/parser/sbt"
)

// Extraction is the declarations read from one admitted file, before they
// become fact envelopes, plus the skip that applies to the file. A manifest's
// Extraction holds its own copy of the few values a declaration needs and no
// reference to the file body, so a caller can keep one per manifest while the
// content walk continues and the body is collected. A pin file's declarations
// still share the body they were read from; the collector keeps that body
// anyway, and the readers here do not hand a pin Extraction out for keeping.
// The zero value holds nothing.
type Extraction struct {
	// sourceKind is the file-level source kind of a pin file; a manifest
	// declaration carries its own section kind.
	sourceKind   string
	declarations []declaration
	skip         Skip
}

// Skip reports why the file was skipped, or that values were dropped from it.
func (e Extraction) Skip() Skip { return e.skip }

// Len is the number of declarations the file will emit.
func (e Extraction) Len() int { return len(e.declarations) }

// Extract reads one admitted file body into an Extraction. ok is false when
// relativePath is not admitted (see Admitted), so a credential carrier never
// reaches a parser. The byte cap is checked before any parsing.
func Extract(relativePath, body string) (Extraction, bool) {
	if sourceKind, ok := SourceKindForPath(relativePath); ok {
		return extractPinFile(sourceKind, body), true
	}
	switch manifestKindForPath(relativePath) {
	case manifestNone:
		return Extraction{}, false
	case manifestPackageJSON:
		return extractManifest(body, func() ([]declaration, int, error) { return extractPackageJSON(body) }), true
	case manifestComposerJSON:
		return extractManifest(body, func() ([]declaration, int, error) { return extractComposer(body, false) }), true
	case manifestComposerLock:
		return extractManifest(body, func() ([]declaration, int, error) { return extractComposer(body, true) }), true
	case manifestGoMod:
		return extractManifest(body, func() ([]declaration, int, error) {
			declarations, rejected := extractGoMod(body)
			return declarations, rejected, nil
		}), true
	case manifestDockerfile:
		return extractManifest(body, func() ([]declaration, int, error) {
			declarations, rejected := extractDockerfile(body)
			return declarations, rejected, nil
		}), true
	case manifestWorkflow:
		return extractManifest(body, func() ([]declaration, int, error) { return extractWorkflow(body) }), true
	case manifestPythonTOML:
		return extractPythonTOMLManifest(relativePath, body), true
	case manifestSetupCFG:
		return extractPythonSetupCFG(body), true
	case manifestSetupPY:
		return extractPythonSetupPY(body), true
	case manifestPom:
		return ExtractJVMEvidence(mavenparser.ExtractRuntimeEvidence([]byte(body))), true
	case manifestGradle:
		return ExtractJVMEvidence(gradleparser.ExtractRuntimeEvidence([]byte(body))), true
	case manifestSbt:
		return ExtractJVMEvidence(sbtparser.ExtractRuntimeEvidence([]byte(body))), true
	case manifestSbtBuildProperties:
		return ExtractJVMEvidence(sbtparser.ExtractBuildPropertiesEvidence([]byte(body))), true
	case manifestDotNetGlobal:
		return ExtractDotNetEvidence(parserjson.ExtractDotNetGlobalEvidence([]byte(body))), true
	case manifestDotNetProject:
		return ExtractDotNetEvidence(nuget.ExtractDotNetProjectEvidence([]byte(body))), true
	case manifestGemfile, manifestGemfileLock:
		if len(body) > MaxManifestBytesPerFile {
			return CappedRubyExtraction(len(body)), true
		}
		return ExtractRubyEvidence(rubyparser.ExtractBundlerRuntimeEvidence(path.Base(relativePath), []byte(body))), true
	}
	return Extraction{}, false
}

// extractPinFile reads a version-pin file body.
func extractPinFile(sourceKind, body string) Extraction {
	if len(body) > MaxBytesPerFile {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: len(body), ByteCap: MaxBytesPerFile}}
	}
	if sourceKind == sourceKindRuntimeTXT {
		return extractRuntimeTXT(body)
	}
	declarations, total := parse(sourceKind, body, MaxTokensPerFile)
	switch {
	case total == 0:
		return Extraction{}
	case total > MaxTokensPerFile:
		return Extraction{skip: Skip{Reason: SkipReasonTokenCap, ByteSize: len(body), TokenCount: total}}
	}
	if sourceKind == sourceKindPHPVersion {
		kept := declarations[:0]
		rejected := 0
		for _, d := range declarations {
			if !safeSpec(d.rawSpec) {
				rejected++
				continue
			}
			kept = append(kept, d)
		}
		if rejected > 0 {
			return Extraction{sourceKind: sourceKind, declarations: kept, skip: Skip{Reason: SkipReasonValueRejected, ByteSize: len(body), Rejected: rejected}}
		}
		declarations = kept
	}
	return Extraction{sourceKind: sourceKind, declarations: declarations}
}

// extractManifest runs one manifest reader under the manifest byte cap and maps
// its outcome onto a skip.
func extractManifest(body string, read func() ([]declaration, int, error)) Extraction {
	if len(body) > MaxManifestBytesPerFile {
		return Extraction{skip: Skip{Reason: SkipReasonByteCap, ByteSize: len(body), ByteCap: MaxManifestBytesPerFile}}
	}
	declarations, rejected, err := read()
	if err != nil {
		return Extraction{skip: Skip{Reason: SkipReasonManifestUnparseable, ByteSize: len(body)}}
	}
	extraction := Extraction{declarations: detach(declarations)}
	if rejected > 0 {
		extraction.skip = Skip{Reason: SkipReasonValueRejected, ByteSize: len(body), Rejected: rejected}
	}
	return extraction
}

// detach copies every string a declaration keeps out of the manifest body. The
// readers hand out substrings of the body (the JSON scanner's rawString, a
// TrimSpace result, a go.mod line slice), and a Go substring keeps the whole
// backing array reachable, so a kept declaration would otherwise pin a body of
// up to MaxManifestBytesPerFile for as long as the collector holds the
// Extraction. It runs once per declaration, after the credential guard, on the
// few values that survive it.
func detach(declarations []declaration) []declaration {
	for i := range declarations {
		d := &declarations[i]
		d.runtime = strings.Clone(d.runtime)
		d.rawSpec = strings.Clone(d.rawSpec)
		if d.toolName != nil {
			name := strings.Clone(*d.toolName)
			d.toolName = &name
		}
		if d.distribution != nil {
			dist := strings.Clone(*d.distribution)
			d.distribution = &dist
		}
	}
	return declarations
}
