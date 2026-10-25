// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"path"
	"strings"
)

// Source kinds for the manifest files. Unlike a pin file, one manifest holds
// several declaring sections, and a reader needs the section to tell a
// constraint (engines) from a pin (volta, packageManager, the go directive), so
// the source kind names the section rather than the file.
const (
	sourceKindPackageJSONEngines = "package_json_engines"
	sourceKindPackageJSONVolta   = "package_json_volta"
	sourceKindPackageJSONManager = "package_json_package_manager"
	sourceKindGoModGo            = "go_mod_go"
	sourceKindGoModToolchain     = "go_mod_toolchain"
)

// section identifies the manifest section a declaration was read from.
type section uint8

const (
	sectionNone section = iota
	sectionPackageJSONEngines
	sectionPackageJSONVolta
	sectionPackageJSONManager
	sectionGoModGo
	sectionGoModToolchain
)

// sectionSourceKinds maps a section to its source kind.
var sectionSourceKinds = [...]string{
	sectionNone:               "",
	sectionPackageJSONEngines: sourceKindPackageJSONEngines,
	sectionPackageJSONVolta:   sourceKindPackageJSONVolta,
	sectionPackageJSONManager: sourceKindPackageJSONManager,
	sectionGoModGo:            sourceKindGoModGo,
	sectionGoModToolchain:     sourceKindGoModToolchain,
}

// MaxManifestBytesPerFile bounds one package.json or go.mod body. A manifest
// is read for a few fixed keys, and the scan allocates for none of the rest, so
// the cap bounds time, not memory. Real manifests are a few KiB; a very large
// monorepo root with thousands of dependencies stays under 256 KiB. A body over
// the cap is skipped whole and reported, never read as a prefix.
const MaxManifestBytesPerFile = 1 << 20

// manifestKind names an admitted manifest file.
type manifestKind int

const (
	manifestNone manifestKind = iota
	manifestPackageJSON
	manifestGoMod
)

// dependencyDirs are directory names whose manifests belong to a dependency,
// not to the repository. Discovery already prunes them (defaultIgnoredDirs in
// the git collector), but a PreservedPathGlobs rule can bring one back, and a
// dependency's own engines field is never this repository's declaration, so
// the emitter refuses them as well.
var dependencyDirs = map[string]struct{}{
	"node_modules":     {},
	"vendor":           {},
	"bower_components": {},
	"jspm_packages":    {},
}

// manifestKindForPath returns the manifest kind of a repository-relative path:
// the exact base name package.json or go.mod at any depth, outside a
// dependency directory. The match is case-sensitive, as npm and the go tool
// are.
func manifestKindForPath(relativePath string) manifestKind {
	return manifestKindForBase(path.Base(relativePath), relativePath)
}

// manifestKindForBase is manifestKindForPath with the base name already
// computed, so a caller that also needs the pin check pays for it once.
func manifestKindForBase(base, relativePath string) manifestKind {
	var kind manifestKind
	switch base {
	case "package.json":
		kind = manifestPackageJSON
	case "go.mod":
		kind = manifestGoMod
	default:
		return manifestNone
	}
	dir := path.Dir(path.Clean(relativePath))
	for dir != "." && dir != "/" && dir != "" {
		if _, skip := dependencyDirs[path.Base(dir)]; skip {
			return manifestNone
		}
		dir = path.Dir(dir)
	}
	return kind
}

// FileClass says how this package reads a repository file.
type FileClass int

const (
	// FileNotAdmitted is any file this package does not read.
	FileNotAdmitted FileClass = iota
	// FilePin is a version-pin file; its body is parsed at emission.
	FilePin
	// FileManifest is a package.json or go.mod outside a dependency
	// directory; the content walk reads it when it meets it.
	FileManifest
)

// ClassifyPath reports how relativePath is read. It is the per-content-file
// check of the Git collector's walk, so it takes one base-name computation and
// no allocation, and a file that is neither costs a map lookup and a switch.
func ClassifyPath(relativePath string) FileClass {
	base := path.Base(relativePath)
	if _, ok := pinFileSourceKinds[base]; ok {
		return FilePin
	}
	if manifestKindForBase(base, relativePath) != manifestNone {
		return FileManifest
	}
	return FileNotAdmitted
}

// Value bounds for a manifest declaration. A version or range is short; a
// longer value, or one holding an opaque token, is not read as a version.
const (
	// maxSpecBytes bounds one raw_spec read from a manifest.
	maxSpecBytes = 128
	// maxOpaqueRun is the length at which a run of letters, digits and
	// underscores in a manifest value is treated as opaque. Real specs ("20.11.1", "lts/hydrogen",
	// "workspace:*", ">=18.17.0-rc.1") stay under it; an API key, a token or a
	// hash does not.
	maxOpaqueRun = 16
)

// safeSpec reports whether raw may be emitted as a manifest raw_spec. The
// manifests are admitted with the store content policy because the named
// sections hold only versions, but the key set is the file's, not ours: a
// credential pasted into engines or packageManager must never leave the file
// in a fact. A value passes only when it is short, plain ASCII from the version
// and range grammar, holds no opaque run, and is not a URL. Everything else is
// dropped and counted, never truncated or masked, because a masked fact would
// still claim a declaration the file does not make.
func safeSpec(raw string) bool {
	if raw == "" || len(raw) > maxSpecBytes || strings.Contains(raw, "://") {
		return false
	}
	run := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			run++
			if run >= maxOpaqueRun {
				return false
			}
			continue
		case strings.IndexByte(" .+*^~<>=|,-/:@${}", c) >= 0:
			run = 0
		default:
			return false
		}
	}
	return true
}
