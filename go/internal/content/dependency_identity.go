// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/crypto/blake2s"
)

// dependencyIdentityPackageManagers is the set of metadata["package_manager"]
// values whose manifest (non-lockfile) dependency Variables qualify for the
// section-keyed CanonicalDependencyEntityID scheme. See
// CanonicalEntityIDWithMetadata for the full gate.
//
// "npm" and "composer" are #5357's original two, proven because a JSON object
// key is unique within its enclosing section by construction. #5507 (this
// file's remaining nine cases) extends the scheme to cargo, gradle, maven,
// nuget, pypi, go (gomod), rubygems, pub, and hex — each proven either by the
// same "the parser's own name field is a unique map/table key, or the
// ecosystem's own tooling rejects a duplicate" argument (rubygems, pub, hex)
// or by an added dependencyIdentityDiscriminator component that keeps a
// same-named, same-section redeclaration distinct (cargo, gradle, maven,
// nuget, pypi, go; see that function's doc comment for the concrete manifest
// feature each discriminator defends). "go" (gomod) needs a discriminator
// too, not just a unique-key argument: golang.org/x/mod's modfile.Parse does
// NOT de-duplicate require directives (that is left to higher-level MVS
// logic), so a hand-edited or merge-conflicted go.mod that has not been run
// through `go mod tidy` can legitimately contain the same module path
// required twice in one section at two different versions.
//
// "swift" is deliberately NOT in this set. The only dependency-row producer
// for package_manager=="swift" is Package.resolved
// (parser/json/swift_package_resolved.go), which sets metadata["lockfile"] =
// true on every row — it is a resolved lockfile, not a manifest, and is
// already correctly excluded by the lockfile condition below. There is no
// Package.swift manifest dependency-row producer in this codebase today,
// so there is nothing in scope to migrate; adding "swift" here would be a
// no-op until such a producer exists, and adding it without a fresh
// uniqueness proof for that producer would be exactly the kind of
// unproven-widening this gate exists to prevent.
//
// Do not extend this set without proving the target format's parser
// guarantees per-section name uniqueness (directly, or through
// dependencyIdentityDiscriminator).
var dependencyIdentityPackageManagers = map[string]struct{}{
	"npm":      {},
	"composer": {},
	"cargo":    {},
	"gradle":   {},
	"maven":    {},
	"nuget":    {},
	"pypi":     {},
	"go":       {},
	"rubygems": {},
	"pub":      {},
	"hex":      {},
}

// CanonicalEntityIDWithMetadata returns the canonical content-entity
// identifier for entityType/entityName at lineNumber, routing in-scope
// manifest dependency Variables to the section-keyed, line-independent
// CanonicalDependencyEntityID and everything else to the legacy line-keyed
// CanonicalEntityID.
//
// An entity qualifies for the dependency form IFF ALL of:
//
//  1. entityType is "variable" (case-insensitive, matching CanonicalEntityID's
//     own normalization);
//  2. metadata["config_kind"] == "dependency";
//  3. metadata["package_manager"] is a member of
//     dependencyIdentityPackageManagers;
//  4. metadata["lockfile"] is absent or explicitly false. This is fail-safe:
//     only an absent key or a recognized false value (native bool false, or
//     the strings "false"/"" after trimming) passes; ANY other present value —
//     bool true, string "true", but also an unrecognized truthy scalar (JSON
//     number 1, "1", "yes", nil, an unexpected type) — fails this condition and
//     falls back to the line-keyed id. See metadataLockfileAbsentOrFalse;
//  5. metadata["section"], trimmed, is a non-empty string.
//
// This narrow gate exists because metadata["config_kind"] == "dependency"
// alone is also emitted by lockfile parsers, which legitimately repeat a
// package name multiple times within one section (nested transitive
// versions). Collapsing those rows under (path, section, name) would
// silently merge distinct dependency versions into one identity, an accuracy
// violation. Condition 4 keeps every current and future lockfile producer out
// regardless of package_manager.
//
// When a row qualifies, the name hashed into CanonicalDependencyEntityID is
// entityName alone for the #5357 npm/composer producers (byte-identical to
// the original, already-shipped scheme), or entityName plus a
// package-manager-specific discriminator from dependencyIdentityDiscriminator
// for the #5507 producers that cannot guarantee (section, name) uniqueness on
// their own. See dependencyIdentityDiscriminator's doc comment for exactly
// which manifest feature each discriminator defends and why an empty
// discriminator is safe for the rest.
//
// Anything that does not satisfy all five conditions returns
// CanonicalEntityID unchanged, so code Variables, Functions, tsconfig rows,
// lockfile rows, and out-of-scope manifest formats keep their existing
// line-keyed identity.
func CanonicalEntityIDWithMetadata(
	repoID string,
	relativePath string,
	entityType string,
	entityName string,
	lineNumber int,
	metadata map[string]any,
) string {
	section, ok := dependencyIdentitySection(entityType, metadata)
	if !ok {
		return CanonicalEntityID(repoID, relativePath, entityType, entityName, lineNumber)
	}

	name := entityName
	packageManager := metadataStringValue(metadata, "package_manager")
	if discriminator := dependencyIdentityDiscriminator(packageManager, metadata); discriminator != "" {
		// U+241E-style separator (ASCII Unit Separator, 0x1F) between the
		// declared name and its discriminator: it cannot appear in any real
		// dependency name/classifier/extras value, so there is no way for two
		// distinct (name, discriminator) pairs to collide by smuggling the
		// separator into one field.
		name = entityName + "\x1f" + discriminator
	}
	return CanonicalDependencyEntityID(repoID, relativePath, section, name)
}

// dependencyIdentitySection applies the five-condition gate documented on
// CanonicalEntityIDWithMetadata and returns the trimmed section name when the
// entity qualifies for section-keyed dependency identity.
func dependencyIdentitySection(entityType string, metadata map[string]any) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(entityType), "variable") {
		return "", false
	}
	if metadataStringValue(metadata, "config_kind") != "dependency" {
		return "", false
	}
	if _, ok := dependencyIdentityPackageManagers[metadataStringValue(metadata, "package_manager")]; !ok {
		return "", false
	}
	if !metadataLockfileAbsentOrFalse(metadata) {
		return "", false
	}
	section := strings.TrimSpace(metadataStringValue(metadata, "section"))
	if section == "" {
		return "", false
	}
	return section, true
}

// dependencyIdentityDiscriminator returns the extra, package-manager-specific
// identity component folded into entityName before it reaches
// CanonicalDependencyEntityID. #5357 proved that (section, name) alone is a
// safe key when the manifest format's parser guarantees name uniqueness
// within a section — an npm/composer JSON object key, a Cargo/pub TOML/YAML
// table key, a Ruby/Elixir/Go manifest whose own tooling rejects a duplicate
// declaration. Several formats extended by #5507 do NOT make that guarantee:
// the same declared name can legitimately repeat within one section for a
// different, coexisting reason. This function returns the metadata field(s)
// that keep those declarations distinct.
//
// An empty return means "no additional discriminator: (section, name) is
// already unique for this package_manager" — this is the path for every
// producer not named below, including npm, composer, rubygems (Gemfile), pub
// (pubspec.yaml), and hex (mix.exs); see dependencyIdentityPackageManagers for
// why each of those is safe without one.
//
// Do not add or change a case here without documenting, in this comment, the
// concrete manifest feature that makes the discriminator necessary — the
// motivating scenario is the proof that the new key does not merge two
// genuinely different dependencies into one identity.
func dependencyIdentityDiscriminator(packageManager string, metadata map[string]any) string {
	switch packageManager {
	case "cargo":
		// A Cargo manifest can depend on the same crate twice under two
		// different local aliases via the `package = "..."` inline-table key
		// (e.g. `tokio1 = { package = "tokio", version = "1" }` alongside
		// `tokio02 = { package = "tokio", version = "0.2" }` in the same
		// [dependencies] table) to bridge two major versions at once. Row
		// "name" is the resolved crate name (cargoDependencySpec.PackageName),
		// which collides for both aliases; "manifest_name" is the TOML table
		// key, which Cargo/TOML guarantees is unique within one section, so it
		// is the correct discriminator.
		return metadataStringValue(metadata, "manifest_name")
	case "go":
		// golang.org/x/mod's modfile.Parse (called by
		// gomod/parser.go's parseGoMod) does NOT de-duplicate require
		// directives — its own docs say that is left to higher-level MVS
		// logic. A hand-edited or merge-conflicted go.mod that has not been
		// run through `go mod tidy` can therefore legitimately contain the
		// same module path required twice in one require/require-indirect
		// section at two different versions (e.g. both
		// `github.com/pkg/errors v0.9.1` and `github.com/pkg/errors v0.8.0`
		// inside one `require (...)` block). Both rows share (section, name);
		// "value" (the row's raw declared version, BEFORE any `replace`
		// substitution) keeps them distinct. The post-replace
		// "resolved_version" would be unsafe here: a version-unconstrained
		// `replace` directive (an empty Old.Version) matches every requested
		// version of a module, so two duplicate requires at different
		// declared versions could both resolve to the identical replacement
		// target/version and collapse right back together — the raw
		// declared version never has that problem.
		return metadataStringValue(metadata, "value")
	case "gradle":
		// The same `group:artifact` coordinate can legitimately be declared
		// twice under the identical configuration — for example a pinned
		// exact version alongside a looser range added later without
		// removing the first line, or a dependency substitution rule
		// exercised twice with different resolved versions. "value" carries
		// the resolved (or raw, if unresolved) version string and is
		// line-independent, so it distinguishes those declarations while
		// staying stable under reordering.
		return metadataStringValue(metadata, "value")
	case "maven":
		// The same groupId:artifactId is routinely declared more than once
		// within one <dependencies>/scope section with a different
		// <classifier> (e.g. netty-tcnative's linux-x86_64 vs osx-x86_64
		// native builds, declared side by side because both are needed) or
		// <type> (e.g. a jar plus its test-jar). An absent <type> defaults to
		// Maven's own default ("jar") so adding an explicit
		// `<type>jar</type>` to an existing implicit-jar dependency does not
		// churn its identity.
		classifier := metadataStringValue(metadata, "dependency_classifier")
		depType := metadataStringValue(metadata, "dependency_type")
		if depType == "" {
			depType = "jar"
		}
		return classifier + "\x1e" + depType
	case "nuget":
		// A .csproj multi-targets by declaring the SAME PackageReference name
		// more than once across different ItemGroups gated on
		// `$(TargetFramework)`, each potentially at a different version (e.g.
		// Newtonsoft.Json pinned to 9.0.1 for net472 and 13.0.1 for net6.0).
		// The row's "section" is always the fixed literal "PackageReference"
		// regardless of which ItemGroup it came from, so section cannot
		// disambiguate on its own.
		//
		// "condition" is nugetProjectDependencyRow's
		// firstNonEmpty(reference.Condition, groupCondition) — an OVERRIDE
		// (item-level Condition wins when present; the group-level Condition
		// is used only as a fallback when the item has none), NOT an
		// AND-combination of both. This is exactly right for, and
		// disambiguates, the common multi-targeting pattern: Condition set
		// once per ItemGroup (per TFM) and never repeated per item. It has a
		// narrow residual gap, accepted for now rather than fixed here: if an
		// item-level Condition is present on two PackageReference rows of the
		// same name AND that item-level string happens to be identical across
		// two ItemGroups with DIFFERENT group-level Conditions, the differing
		// group-level TFM distinction is masked by the override and the two
		// rows would collide. Closing that gap would require the parser to
		// expose the item- and group-level Condition components as separate
		// metadata fields instead of pre-merging them (nugetProjectDependencyRow
		// in internal/parser/nuget_project_language.go) — out of scope for
		// this identity-layer discriminator, and a rare combination in
		// practice since per-item Conditions on individual PackageReference
		// elements are uncommon.
		return metadataStringValue(metadata, "condition")
	case "pypi":
		// A pip/PEP 508 requirement (requirements.txt lines, and the
		// PEP 621/Hatch array-form pyproject.toml dependency lists) can
		// legitimately repeat the same package name within one section with
		// different extras (`requests[socks]` vs `requests[toml]`) or
		// environment markers (`foo; sys_platform=="win32"` vs
		// `foo; sys_platform=="linux"`) declared side by side to cover
		// different install contexts simultaneously. Poetry/Hatch
		// TABLE-form dependencies key by TOML map key and are already unique
		// without this; adding the same discriminator there is harmless
		// (extras/marker are typically absent, so it is an empty suffix).
		//
		// This discriminator deliberately does NOT include "value" (the
		// version specifier/constraint), unlike gradle's and go's version
		// discriminators. Two requirement lines for the same package in the
		// same section with the same extras/marker but different version
		// constraints — e.g. `requests>=2` on one line and `requests<3` on
		// another — are not two competing declarations the way two gomod
		// `require` lines at different pinned versions are: pip's resolver
		// (both the legacy and the default 2020 resolver) combines every
		// constraint it finds for one canonical package name into a single
		// intersected specifier set (`>=2,<3` here) before resolving one
		// install candidate. They are ONE logical dependency split across
		// two lines, not two. Collapsing them to one content-entity id is
		// therefore the semantically correct outcome, not an oversight —
		// see TestCanonicalEntityIDWithMetadataPyPIConstraintValueIntentionallyOmittedFromDiscriminator.
		// The surviving row when two lines collapse is deterministic: shape.
		// Materialize sorts entities by line number before minting ids, and
		// internal/storage/postgres's deduplicateEntityRows keeps the LAST
		// occurrence in input order, so the physically later requirement
		// line in the file is always the one whose other fields (SourceCache,
		// StartLine, ...) survive.
		return dependencyExtrasAndMarker(metadata)
	default:
		return ""
	}
}

// dependencyExtrasAndMarker joins metadata["extras"] (sorted, so declaration
// order never changes the discriminator) and metadata["marker"] into the
// pypi identity discriminator. Both fields are stable across a manifest
// reorder and absent from every other package_manager's rows.
func dependencyExtrasAndMarker(metadata map[string]any) string {
	extras := metadataStringSliceValue(metadata, "extras")
	sort.Strings(extras)
	marker := metadataStringValue(metadata, "marker")
	if len(extras) == 0 && marker == "" {
		return ""
	}
	return strings.Join(extras, ",") + "\x1e" + marker
}

// metadataStringValue reads a string-typed metadata value, returning "" for a
// missing key or a value of any other type.
func metadataStringValue(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// metadataStringSliceValue reads a string-slice metadata value, accepting
// both a native []string (the collector snapshot mint site's shape) and a
// []any of strings (the shape a JSON-decoded fact-replay payload produces).
// Any other type, or a missing key, returns nil.
func metadataStringSliceValue(metadata map[string]any, key string) []string {
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, element := range typed {
			if text, ok := element.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// metadataLockfileAbsentOrFalse reports whether a row's "lockfile" metadata
// permits section-keyed dependency identity. It is deliberately fail-safe:
// section-keying is allowed ONLY when the key is absent, or its value is a
// recognized false (native bool false, or the strings "false"/"" after
// trimming, case-insensitive). ANY other present value — bool true, string
// "true", but also an unrecognized truthy scalar a future producer might emit
// (JSON number 1, "1", "yes", nil, an unexpected type) — returns false so the
// caller falls back to the legacy line-keyed CanonicalEntityID.
//
// This is the load-bearing safety check of the whole dependency-identity gate:
// the section-keyed form collapses (path, section, name[, discriminator]) to
// one id, which is correct only for manifests that guarantee per-section
// uniqueness. Lockfiles do not — an npm lockfile legitimately repeats a
// package name across nested node_modules at different versions — so
// admitting a lockfile row would merge react@17 and react@18 into one
// identity, a hard accuracy violation. Falling back to the line-keyed id is
// the safe direction: it never merges distinct entities, it only risks
// line-churn (which is exactly the churn this feature removes for real
// manifest rows).
func metadataLockfileAbsentOrFalse(metadata map[string]any) bool {
	value, present := metadata["lockfile"]
	if !present {
		return true
	}
	switch typed := value.(type) {
	case bool:
		return !typed
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed == "" || strings.EqualFold(trimmed, "false")
	default:
		return false
	}
}

// CanonicalDependencyEntityID returns the section-keyed, line-independent
// content-entity identifier for an in-scope manifest dependency Variable (see
// CanonicalEntityIDWithMetadata's gate). Reordering dependencies within a
// manifest section, or a source line shifting because of an unrelated edit
// elsewhere in the file, does not change this identity — unlike the
// line-keyed CanonicalEntityID.
//
// The hash input is domain-tagged ("eshu-dep-v1") and six newline-joined
// components wide: the tag, repoID, relativePath, the constant "variable",
// section, and name. CanonicalEntityID's input is five components with no
// tag, so a dependency Variable's identity can never collide with a code
// Variable's identity for the same (repo, path, name) — the tag plus the
// differing component count give unconditional domain separation. Callers
// that need a package-manager-specific discriminator (see
// dependencyIdentityDiscriminator) fold it into the name argument before
// calling this function; CanonicalDependencyEntityID itself is unaware of the
// concept and its hash shape never changes, so the #5357 npm/composer ids
// this function already minted in production stay byte-identical.
func CanonicalDependencyEntityID(repoID, relativePath, section, name string) string {
	identity := fmt.Sprintf(
		"eshu-dep-v1\n%s\n%s\n%s\n%s\n%s",
		strings.TrimSpace(repoID),
		strings.TrimSpace(relativePath),
		"variable",
		strings.TrimSpace(section),
		strings.TrimSpace(name),
	)
	sum := blake2s.Sum256([]byte(identity))
	return fmt.Sprintf("content-entity:e_%s", hex.EncodeToString(sum[:])[:12])
}
