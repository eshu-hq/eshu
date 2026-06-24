// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
)

// normalizeImportSource maps a per-file import source string to an
// (ecosystem, coordinate) pair suitable for owner lookup against the
// package-registry decision space.
//
// Rules per ecosystem:
//
//   - javascript / typescript (npm): relative paths (starts with "./" or "../")
//     are intra-repo; bare specifiers that contain a path separator only when the
//     path looks like a local alias (resolved_source is non-empty in that case)
//     are dropped conservatively. A bare name or scoped @scope/name is kept.
//   - python (pypi): relative imports start with "./" or are the short form
//     ".sibling" (leading dot, no slash) — both dropped. Subpackages are
//     normalized to the top-level distribution name (first dot-separated segment).
//   - go (gomod): stdlib paths contain no dot in the host segment; dropped.
//     Everything else is kept as-is (caller applies subpath resolution via
//     matchImportCoordinateToOwner).
//
// Unknown languages return empty strings and are not further processed.
// Empty source always returns empty strings.
func normalizeImportSource(source, language string) (ecosystem, coordinate string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", ""
	}

	switch strings.ToLower(language) {
	case "javascript", "typescript":
		return normalizeNPMImportSource(source)
	case "python":
		return normalizePyPIImportSource(source)
	case "go":
		return normalizeGoImportSource(source)
	}
	return "", ""
}

// normalizeNPMImportSource classifies a JavaScript/TypeScript import source and
// returns its npm package-root coordinate.
//
// Relative paths ("./", "../"), node built-in protocol ("node:"), and empty
// specifiers are dropped. For a scoped specifier ("@scope/name/sub") the package
// root is "@scope/name"; for a bare specifier ("lodash/fp") the package root is
// the first path segment ("lodash"). A bare multi-segment specifier that is
// actually an intra-repo baseUrl import (e.g. "src/components/Button") is still
// reduced to its first segment ("src"); since no indexed package owns "src" the
// owner lookup misses and the import is dropped conservatively rather than
// guessed.
func normalizeNPMImportSource(source string) (string, string) {
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
		return "", ""
	}
	// Node built-in protocol (node:fs) — not an npm package.
	if strings.HasPrefix(source, "node:") {
		return "", ""
	}
	return "npm", npmPackageRoot(source)
}

// npmPackageRoot reduces an npm import specifier to its package-root coordinate.
// Scoped packages keep the first two segments ("@scope/name"); unscoped packages
// keep the first segment ("name").
func npmPackageRoot(source string) string {
	segments := strings.Split(source, "/")
	if strings.HasPrefix(source, "@") {
		if len(segments) >= 2 {
			return segments[0] + "/" + segments[1]
		}
		return source
	}
	return segments[0]
}

// normalizePyPIImportSource classifies a Python import source string.
// Python relative imports are identified by a leading "./" or a leading "."
// that is not followed by a letter or digit (the intra-package dotted form
// the parser emits). Subpackages are normalized to the top-level name.
func normalizePyPIImportSource(source string) (string, string) {
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
		return "", ""
	}
	// Parser-emitted relative form: starts with "." (e.g. ".sibling").
	if strings.HasPrefix(source, ".") {
		return "", ""
	}
	// Normalize subpackage to top-level distribution name.
	if idx := strings.Index(source, "."); idx > 0 {
		return "pypi", source[:idx]
	}
	return "pypi", source
}

// normalizeGoImportSource classifies a Go import path.
// Standard library packages have no dot in the first path segment (e.g. "fmt",
// "net/http"). Everything else is treated as a module path.
func normalizeGoImportSource(source string) (string, string) {
	first := source
	if idx := strings.Index(source, "/"); idx >= 0 {
		first = source[:idx]
	}
	if !strings.Contains(first, ".") {
		// No dot in host segment → stdlib.
		return "", ""
	}
	return "gomod", source
}

// matchImportCoordinateToOwner returns the owning repository ID for the given
// (ecosystem, coordinate) pair by consulting the codeImportOwnerIndex built from
// package-registry identity facts joined to exact/derived ownership and
// publication decisions.
//
// The lookup reuses the sanctioned (ecosystem, name) join key produced by
// packageConsumptionKeys — the same correctness basis #3598 ships in production
// via BuildPackageConsumptionDecisions. It applies two tolerance rules for Go
// module paths before consulting the index:
//
//  1. Exact coordinate: look up the coordinate as-is.
//  2. Go module subpath / version suffix: strip trailing path segments one at a
//     time (which also removes a trailing /vN major-version suffix) until a
//     registered module coordinate matches. This resolves
//     "github.com/foo/bar/internal/x" and "github.com/foo/bar/v2" to
//     "github.com/foo/bar".
//
// For npm and pypi only the exact coordinate is consulted because
// normalizeImportSource already reduced the import source to the canonical
// package-root name.
//
// An empty ecosystem or coordinate, or a coordinate with no owner, returns "".
// Ambiguous outcomes are never guessed: the index admits only exact/derived
// owners via packageOwnerOutcomeAdmits, and a coordinate that maps to more than
// one owning repository is dropped as ambiguous.
func matchImportCoordinateToOwner(ecosystem, coordinate string, owners codeImportOwnerIndex) string {
	ecosystem = strings.TrimSpace(ecosystem)
	coordinate = strings.TrimSpace(coordinate)
	if ecosystem == "" || coordinate == "" || owners.empty() {
		return ""
	}

	for _, candidate := range codeImportCoordinateCandidates(ecosystem, coordinate) {
		if repoID := owners.lookup(ecosystem, candidate); repoID != "" {
			return repoID
		}
	}
	return ""
}

// codeImportCoordinateCandidates returns the ordered coordinate candidates to
// try for one import source. npm and pypi yield just the coordinate. Go module
// paths additionally yield each parent path obtained by stripping trailing
// segments, so subpaths and /vN version suffixes resolve to the module root.
func codeImportCoordinateCandidates(ecosystem, coordinate string) []string {
	candidates := []string{coordinate}
	if ecosystem != "gomod" {
		return candidates
	}
	remaining := coordinate
	for {
		idx := strings.LastIndex(remaining, "/")
		if idx < 0 {
			break
		}
		remaining = remaining[:idx]
		if remaining == "" {
			break
		}
		candidates = append(candidates, remaining)
	}
	return candidates
}
