// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// runtimeVersionPrefix is the label some tools write before a version in a pin
// file, for example "ruby-3.2.2" in .ruby-version or "go1.22.1" in
// .go-version. It is dropped from the parsed parts only; raw_spec keeps it.
var runtimeVersionPrefix = map[string]string{
	"ruby":   "ruby-",
	"go":     "go",
	"python": "python-",
}

// swiftReleasePattern matches a plain numeric Swift release such as "5.9" or
// "6.0.1". A .swift-version may also name a release tag
// ("swift-5.9-RELEASE"), a development snapshot
// ("5.10-DEVELOPMENT-SNAPSHOT-2024-01-01-a") or a channel snapshot
// ("main-snapshot-2024-01-01"); those stay aliases as written, so a snapshot
// is never reported as the release it precedes.
var swiftReleasePattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)

// MaxTokensPerFile bounds the version tokens one pin file or repeatable
// manifest source may declare. A real pin file holds a handful of tokens (a .tool-versions names one tool per
// line); 1024 leaves wide headroom for a monorepo file that pins many tools,
// and a file over it is generated or hostile content. Emitting a prefix would
// present a partial list as the whole file, so the whole file is skipped and
// the caller is told how many tokens it held.
const MaxTokensPerFile = shared.RuntimeVersionTokenCap

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
	// SkipReasonByteCap marks an admitted file larger than its byte cap:
	// MaxBytesPerFile for a pin file, MaxManifestBytesPerFile for a manifest.
	SkipReasonByteCap = "byte_cap_exceeded"
	// SkipReasonTokenCap marks a file with more than MaxTokensPerFile tokens.
	SkipReasonTokenCap = "token_cap_exceeded"
	// SkipReasonManifestUnparseable marks a package.json that is not readable
	// JSON (malformed, or nested past the depth bound). It emits nothing.
	SkipReasonManifestUnparseable = "manifest_unparseable"
	// SkipReasonValueRejected marks a manifest from which Rejected values were
	// dropped: every string value that did not become a declaration, whether
	// credential-shaped, over the value bound, ambiguous, empty or whitespace
	// only, or a packageManager with no "@", no version, or a name corepack does
	// not permit. Unlike the other reasons the file's other declarations are
	// still emitted: each dropped value is one the file did not declare as a
	// version, not a prefix of the file.
	SkipReasonValueRejected = "value_rejected"
)

// Skip describes why EmitFile emitted nothing for an admitted file (a
// version-pin file or a manifest), or dropped values from it. The zero value
// means the file was not skipped. It carries sizes and counts only,
// never file content.
type Skip struct {
	// Reason is one of the SkipReason constants; empty when not skipped.
	Reason string
	// ByteSize is the body size in bytes.
	ByteSize int
	// ByteCap is the cap a SkipReasonByteCap skip exceeded: MaxBytesPerFile for
	// a pin file, MaxManifestBytesPerFile for a manifest.
	ByteCap int
	// TokenCount is the exact number of tokens the file holds. It is set for
	// SkipReasonTokenCap only: a byte-cap skip never tokenizes the file.
	TokenCount int
	// Rejected is the number of values dropped from a manifest. It is set for
	// SkipReasonValueRejected only.
	Rejected int
}

// Skipped reports whether the file was skipped.
func (s Skip) Skipped() bool { return s.Reason != "" }

// Emit reads one admitted file body (a version-pin file, package.json or
// go.mod) and returns one "runtime_declaration.version" fact envelope per
// declaration. relativePath is the repository-relative path of the file. It
// returns nil when the path is not admitted, so a credential carrier such as
// .npmrc never emits, when the body holds no declaration, and when the file is
// over its byte or token cap or is not readable. Use EmitFile to learn about a
// skip.
func Emit(ctx FixtureContext, repoID, relativePath, body string) []facts.Envelope {
	envelopes, _ := EmitFile(ctx, repoID, relativePath, body)
	return envelopes
}

// EmitFile is Emit plus the skip signal: when the file is over a cap, holds
// more than MaxTokensPerFile tokens, is not readable, or lost values to the
// credential guard, it returns a Skip saying which. A cap, token or readability
// skip emits nothing; a value rejection still emits the file's other
// declarations. The byte cap is checked first, before any parsing; past the
// token cap the parser counts without storing, so memory stays bounded by the
// cap and the longest line, not by the size of the body.
func EmitFile(ctx FixtureContext, repoID, relativePath, body string) (envelopes []facts.Envelope, skip Skip) {
	extraction, ok := Extract(relativePath, body)
	if !ok {
		return nil, Skip{}
	}
	return EmitExtraction(ctx, repoID, relativePath, extraction)
}

// EmitExtraction builds the fact envelopes of an Extraction. It is the second
// half of EmitFile, split so the Git collector can read a manifest when the
// content walk meets it and keep only its few declarations until emission.
//
// Each payload is built from the typed runtimedeclarationv1.Version through
// factschema.EncodeRuntimeDeclarationVersion (Contract System v1 §3.1), never a
// hand-built map. A declaration that fails to encode is skipped rather than
// emitted malformed; this cannot happen in practice since every field is a
// plain string, integer or boolean.
func EmitExtraction(ctx FixtureContext, repoID, relativePath string, extraction Extraction) (envelopes []facts.Envelope, skip Skip) {
	scopeDir := ScopeDir(relativePath)
	envelopes = make([]facts.Envelope, 0, len(extraction.declarations))
	for _, d := range extraction.declarations {
		sourceKind := extraction.sourceKind
		if d.section != sectionNone {
			sourceKind = sectionSourceKinds[d.section]
		}
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
		applySpec(&version, classifyDeclaration(sourceKind, d))
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
	return envelopes, extraction.skip
}

// classifyDeclaration classifies one pin-file declaration. A value that is not
// a list of versions (values.go) and a .swift-version that is not a plain
// numeric release never reach Classify: they are unresolved when they hold a
// placeholder and an alias otherwise.
func classifyDeclaration(sourceKind string, d declaration) Spec {
	if d.whole || (sourceKind == sourceKindSwiftVersion && !swiftReleasePattern.MatchString(d.rawSpec)) {
		return unparsedSpec(d.rawSpec)
	}
	if sourceKind == sourceKindGemfileLockRuby {
		return Classify(strings.TrimPrefix(d.rawSpec, "ruby "))
	}
	if sourceKind == sourceKindRuntimeTXT {
		return Classify(strings.TrimPrefix(d.rawSpec, "python-"))
	}
	return Classify(stripRuntimePrefix(d.runtime, d.rawSpec))
}

// unparsedSpec is the spec of a declaration whose text is kept as written:
// unresolved when it holds an unexpanded placeholder, an alias otherwise.
func unparsedSpec(raw string) Spec {
	if placeholderPattern.MatchString(raw) {
		return Spec{Kind: SpecKindUnresolved}
	}
	return Spec{Kind: SpecKindAlias}
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
