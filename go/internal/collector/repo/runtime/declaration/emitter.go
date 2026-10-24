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

// Emit reads one version-pin file body and returns one
// "runtime_declaration.version" fact envelope per (line, token). relativePath
// is the repository-relative path of the file. It returns nil when the path is
// not an admitted pin file, so a credential carrier such as .npmrc never emits,
// when the body holds no declaration, and when the file is over
// MaxTokensPerFile. Use EmitFile to learn about a cap skip.
func Emit(ctx FixtureContext, repoID, relativePath, body string) []facts.Envelope {
	envelopes, _ := EmitFile(ctx, repoID, relativePath, body)
	return envelopes
}

// EmitFile is Emit plus the cap signal: when the file holds more than
// MaxTokensPerFile tokens it emits nothing and returns the token count as
// skippedTokens (0 otherwise), so the caller can count and log the skip.
//
// Each payload is built from the typed runtimedeclarationv1.Version through
// factschema.EncodeRuntimeDeclarationVersion (Contract System v1 §3.1), never a
// hand-built map. A token that fails to encode is skipped rather than emitted
// malformed; this cannot happen in practice since every field is a plain
// string, integer or boolean.
func EmitFile(ctx FixtureContext, repoID, relativePath, body string) (envelopes []facts.Envelope, skippedTokens int) {
	sourceKind, ok := SourceKindForPath(relativePath)
	if !ok {
		return nil, 0
	}
	declarations := parse(sourceKind, body)
	if len(declarations) == 0 {
		return nil, 0
	}
	if len(declarations) > MaxTokensPerFile {
		return nil, len(declarations)
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
	return envelopes, 0
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
