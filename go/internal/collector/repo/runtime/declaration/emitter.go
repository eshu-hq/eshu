// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

// runtimeVersionPrefix is the label some tools write before a version in a pin
// file, for example "ruby-3.2.2" in .ruby-version or "go1.22.1" in
// .go-version. It is dropped from the parsed parts only; raw_spec keeps it.
var runtimeVersionPrefix = map[string]string{
	"ruby":   "ruby-",
	"go":     "go",
	"python": "python-",
}

// MaxTokensPerFile bounds the version tokens one pin file may declare. A real
// pin file holds a handful of tokens (a .tool-versions names one tool per
// line); 1024 leaves wide headroom for a monorepo file that pins many tools,
// and a file over it is generated or hostile content. Emitting a prefix would
// present a partial list as the whole file, so the whole file is skipped and
// the caller is told how many tokens it held.
const MaxTokensPerFile = 1024

// MaxBytesPerFile bounds the size of one pin file body. It is checked before
// any parsing, so a hostile or generated file costs nothing proportional to its
// size before the skip. The largest realistic pin file is a .tool-versions at
// MaxTokensPerFile tokens, about 10 KiB; 64 KiB leaves six times that headroom
// and is still small next to the documentation body cap (512 KiB). The bound
// also covers a single line and a single token: no raw_spec can be longer than
// the file that holds it. A file over the cap is skipped whole.
const MaxBytesPerFile = 64 * 1024

// Skip reasons reported by EmitFile. They are closed values for a log field,
// never a metric label.
const (
	// SkipReasonByteCap marks a file larger than MaxBytesPerFile.
	SkipReasonByteCap = "byte_cap_exceeded"
	// SkipReasonTokenCap marks a file with more than MaxTokensPerFile tokens.
	SkipReasonTokenCap = "token_cap_exceeded"
)

// Skip describes why EmitFile emitted nothing for an admitted pin file. The
// zero value means the file was not skipped. It carries sizes and counts only,
// never file content.
type Skip struct {
	// Reason is SkipReasonByteCap or SkipReasonTokenCap; empty when not skipped.
	Reason string
	// ByteSize is the body size in bytes.
	ByteSize int
	// TokenCount is the exact number of tokens the file holds. It is set for
	// SkipReasonTokenCap only: a byte-cap skip never tokenizes the file.
	TokenCount int
}

// Skipped reports whether the file was skipped.
func (s Skip) Skipped() bool { return s.Reason != "" }

// Emit reads one version-pin file body and returns one
// "runtime_declaration.version" fact envelope per (line, token). relativePath
// is the repository-relative path of the file. It returns nil when the path is
// not an admitted pin file, so a credential carrier such as .npmrc never emits,
// when the body holds no declaration, and when the file is over MaxBytesPerFile
// or MaxTokensPerFile. Use EmitFile to learn about a cap skip.
func Emit(ctx FixtureContext, repoID, relativePath, body string) []facts.Envelope {
	envelopes, _ := EmitFile(ctx, repoID, relativePath, body)
	return envelopes
}

// EmitFile is Emit plus the cap signal: when the file is over MaxBytesPerFile
// or holds more than MaxTokensPerFile tokens it emits nothing and returns a
// Skip saying which cap tripped, so the caller can count and log the skip.
// The byte cap is checked first, before any parsing; past the token cap the
// parser counts without storing, so memory stays bounded by the cap and the
// longest line, not by the size of the body.
//
// Each payload is built from the typed runtimedeclarationv1.Version through
// factschema.EncodeRuntimeDeclarationVersion (Contract System v1 §3.1), never a
// hand-built map. A token that fails to encode is skipped rather than emitted
// malformed; this cannot happen in practice since every field is a plain
// string, integer or boolean.
func EmitFile(ctx FixtureContext, repoID, relativePath, body string) (envelopes []facts.Envelope, skip Skip) {
	sourceKind, ok := SourceKindForPath(relativePath)
	if !ok {
		return nil, Skip{}
	}
	if len(body) > MaxBytesPerFile {
		return nil, Skip{Reason: SkipReasonByteCap, ByteSize: len(body)}
	}
	declarations, total := parse(sourceKind, body, MaxTokensPerFile)
	if total == 0 {
		return nil, Skip{}
	}
	if total > MaxTokensPerFile {
		return nil, Skip{Reason: SkipReasonTokenCap, ByteSize: len(body), TokenCount: total}
	}
	scopeDir := ScopeDir(relativePath)
	envelopes = make([]facts.Envelope, 0, len(declarations))
	for _, d := range declarations {
		version := runtimedeclarationv1.Version{
			CollectorInstanceID: ctx.CollectorInstanceID,
			RepoID:              repoID,
			RelativePath:        relativePath,
			ScopeDir:            scopeDir,
			LineNumber:          d.line,
			OrderIndex:          d.order,
			SourceKind:          sourceKind,
			Runtime:             d.runtime,
			ToolName:            d.toolName,
			RawSpec:             d.rawSpec,
			Distribution:        d.distribution,
		}
		applySpec(&version, Classify(stripRuntimePrefix(d.runtime, d.rawSpec)))
		payload, err := factschema.EncodeRuntimeDeclarationVersion(version)
		if err != nil {
			continue
		}
		stableKey := facts.StableID(facts.RuntimeDeclarationVersionFactKind, map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"line_number":   d.line,
			"runtime":       d.runtime,
			"order_index":   d.order,
		})
		envelopes = append(envelopes, newEnvelope(ctx, stableKey, relativePath, payload))
	}
	return envelopes, Skip{}
}

// stripRuntimePrefix drops a leading runtime label such as "ruby-" or "go" when
// a digit follows, so "go1.22.1" classifies as a version while "golang" stays a
// name.
func stripRuntimePrefix(runtime, raw string) string {
	prefix, ok := runtimeVersionPrefix[runtime]
	if !ok || !strings.HasPrefix(raw, prefix) {
		return raw
	}
	rest := raw[len(prefix):]
	if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
		return rest
	}
	return raw
}

// applySpec copies a classification onto the payload struct.
func applySpec(version *runtimedeclarationv1.Version, spec Spec) {
	version.SpecKind = spec.Kind
	version.VersionMajor, version.VersionMinor, version.VersionPatch = spec.Major, spec.Minor, spec.Patch
	version.RangeLower, version.RangeUpper = spec.RangeLower, spec.RangeUpper
	version.RangeLowerMajor, version.RangeUpperMajor = spec.RangeLowerMajor, spec.RangeUpperMajor
	version.RangeLowerInclusive, version.RangeUpperInclusive = spec.RangeLowerInclusive, spec.RangeUpperInclusive
}
