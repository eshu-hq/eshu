// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"sort"
	"strings"
)

// DependencyCoverageStatus describes how much vulnerability-impact evidence
// Eshu's repository parser layer can emit for one ecosystem manifest or
// lockfile.
type DependencyCoverageStatus string

const (
	// DependencyCoverageCovered means a Git-side manifest or lockfile parser
	// emits `content_entity` dependency facts that the package consumption
	// reducer can join to package-registry identity for the named file.
	DependencyCoverageCovered DependencyCoverageStatus = "covered"

	// DependencyCoverageGap means the named file is not parsed into a
	// repository dependency fact today. The reducer cannot admit consumption
	// from this evidence source. Missing dependency evidence is neither safe
	// nor affected; it is missing.
	DependencyCoverageGap DependencyCoverageStatus = "gap"
)

// DependencyCoverageEntry describes how a single ecosystem file is handled by
// the repository dependency parser surface that feeds the supply-chain impact
// reducer.
type DependencyCoverageEntry struct {
	// Ecosystem is the canonical package-registry ecosystem identifier (npm,
	// pypi, maven, go, nuget, ruby, rust, php, ...).
	Ecosystem string
	// FilePattern is the lowercase filename Eshu's collector observes inside a
	// repository tree.
	FilePattern string
	// FileKind describes how the file is used by the package manager:
	// "manifest" for human-edited declarations, "lockfile" for resolver
	// output, "build" for build scripts that name dependencies inline.
	FileKind string
	// Status declares whether the file is parsed into repository dependency
	// facts (DependencyCoverageCovered) or still a gap
	// (DependencyCoverageGap).
	Status DependencyCoverageStatus
	// CapturesPackageIdentity is true when a covered parser emits the
	// declared dependency name (manifest spelling) as the fact's
	// `entity_name`. Repository-scoped identity is added by the surrounding
	// content_entity envelope as `repo_id` and `relative_path`.
	CapturesPackageIdentity bool
	// CapturesExactVersion is true when the captured `value` field is the
	// resolver-locked exact version (lockfiles) rather than a manifest
	// range.
	CapturesExactVersion bool
	// CapturesVersionRange is true when the captured `value` field is the
	// human-declared version range or requirement specifier.
	CapturesVersionRange bool
	// CapturesScope is true when the captured `section` distinguishes
	// dependency scopes such as runtime, dev, optional, peer, or
	// build/test.
	CapturesScope bool
	// CapturesDevRuntimeSplit is true when the parser distinguishes dev
	// dependencies from runtime dependencies (a stricter form of
	// CapturesScope used by the consumption reducer to bound impact to
	// production code).
	CapturesDevRuntimeSplit bool
	// CapturesDependencyChain is true when transitive dependency paths are
	// preserved (`dependency_path`, `dependency_depth`,
	// `direct_dependency`).
	CapturesDependencyChain bool
	// SourceReference points to the parser entrypoint or note that justifies
	// the Status field, expressed relative to the repository root so doc
	// tooling and reviewers can audit the claim.
	SourceReference string
	// Notes carries a one-line operator-facing explanation. For gaps it
	// names the safety consequence; for covered entries it names what is or
	// is not yet preserved.
	Notes string
}

// DependencyCoverage returns the authoritative repository dependency parser
// coverage matrix used by docs and tests to describe vulnerability-impact
// readiness. Entries are sorted by ecosystem, then by file kind, then by file
// name so the slice is stable for generated tables.
//
// This matrix is the single source of truth for issue #571's acceptance
// criterion: each ecosystem/file pair is either Covered with an end-to-end
// fixture or marked as an explicit Gap so callers can see that missing
// dependency evidence is not the same as "no vulnerability."
func DependencyCoverage() []DependencyCoverageEntry {
	entries := []DependencyCoverageEntry{
		{
			Ecosystem:               "npm",
			FilePattern:             "package.json",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/json/language.go (dependencyVariables, npm)",
			Notes:                   "dependencies, devDependencies, optionalDependencies, and peerDependencies emit content_entity rows with runtime/dev/optional/peer scope labels.",
		},
		{
			Ecosystem:               "npm",
			FilePattern:             "package-lock.json",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/json/package_lock.go",
			Notes:                   "lockfile v3 and v1 emit exact-version rows with dependency_path/depth, direct_dependency, and runtime/dev/optional/peer scope where npm records it.",
		},
		{
			Ecosystem:               "npm",
			FilePattern:             "yarn.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/nodelockfile/yarn_classic.go and yarn_berry.go",
			Notes:                   "Yarn classic v1 and Yarn Berry lockfiles emit exact-version rows with package_manager=npm and package_manager_flavor=yarn, dependency_path/depth, and direct_dependency; yarn.lock alone does not preserve dev/runtime/optional/peer importer scope, and unsupported non-npm Berry protocols are audit-only rows.",
		},
		{
			Ecosystem:               "npm",
			FilePattern:             "pnpm-lock.yaml",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/nodelockfile/pnpm.go",
			Notes:                   "pnpm v6+ lockfiles emit exact-version rows with package_manager=npm and package_manager_flavor=pnpm, dependency_path/depth, direct_dependency, and runtime vs dev scope split; workspace and file/link/portal entries stay out of remote evidence.",
		},
		{
			Ecosystem:               "composer",
			FilePattern:             "composer.json",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/json/language.go (dependencyVariables, composer)",
			Notes:                   "require and require-dev sections emit content_entity rows.",
		},
		{
			Ecosystem:               "composer",
			FilePattern:             "composer.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/json/composer_lock.go",
			Notes:                   "packages and packages-dev arrays emit exact-version rows with a lockfile flag, runtime/dev scope, and direct/transitive dependency paths when same-section require edges prove the chain.",
		},
		{
			Ecosystem:               "pypi",
			FilePattern:             "pyproject.toml",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/pythondep/pyproject.go",
			Notes:                   "PEP 621 project.dependencies, project.optional-dependencies, tool.poetry.dependencies, tool.poetry.dev-dependencies, tool.poetry.group.*.dependencies, and tool.hatch.envs.*.dependencies emit content_entity rows. VCS/path/url/editable forms surface with non-dependency config_kind so the reducer cannot mis-admit them as registry consumption.",
		},
		{
			Ecosystem:               "pypi",
			FilePattern:             "requirements.txt",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/pythondep/requirements.go",
			Notes:                   "pip requirements files (requirements.txt and requirements-*.txt / requirements_*.txt) emit content_entity rows with extras, environment markers, and a dev_dependency flag derived from filename suffixes such as -dev/-test. VCS/path/url/editable/malformed lines surface as separate config_kind values.",
		},
		{
			Ecosystem:               "pypi",
			FilePattern:             "pipfile",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/pythondep/pipfile.go",
			Notes:                   "[packages] and [dev-packages] tables emit content_entity rows; inline {git=, path=, url=} entries surface with vcs/path/url config_kind so the reducer cannot mis-admit them as registry consumption.",
		},
		{
			Ecosystem:               "pypi",
			FilePattern:             "pipfile.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/json/pipfile_lock.go",
			Notes:                   "Pipenv lockfile default and develop sections emit exact-version rows. Entries with git/path/file source keys surface as vcs/path/url config_kind so registry-version semantics are not invented.",
		},
		{
			Ecosystem:               "pypi",
			FilePattern:             "poetry.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/pythondep/poetry_lock.go",
			Notes:                   "Each [[package]] array entry emits one exact-version row. Attached [package.source] type=git/directory/url swaps the row to vcs/path/url config_kind so non-registry lock entries cannot be treated as PyPI registry versions.",
		},
		{
			Ecosystem:               "go",
			FilePattern:             "go.mod",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			SourceReference:         "go/internal/parser/gomod/parser.go (Parse for go.mod)",
			Notes:                   "require/indirect/replace/exclude directives emit content_entity rows. go.mod is a manifest, not a lockfile: require versions are the MVS minimum-version requirement (and the version the resolver would select when no transitive dep forces a higher one), not a resolver-locked exact installed version, so the matrix tracks them as CapturesVersionRange. Replacement targets surface as resolved_module_path/resolved_version on each require row; replace/exclude rows use distinct config_kind values so they never get admitted as consumption. Go has no dev/runtime split, and module-graph chains beyond direct/indirect are not stored in go.mod.",
		},
		{
			Ecosystem:       "go",
			FilePattern:     "go.sum",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/gomod/parser.go emits checksum-only rows (config_kind=dependency_checksum, ambiguous=true)",
			Notes:           "go.sum records every module version any tool has ever verified, not the currently selected version, so checksum-only ambiguity prevents claiming exact observed installed evidence. The gomod parser emits dependency_checksum rows for audit corroboration but the consumption reducer treats go.sum as missing evidence until paired with a go.mod require.",
		},
		{
			Ecosystem:               "maven",
			FilePattern:             "pom.xml",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/maven/parser.go",
			Notes:                   "<dependencies> and <dependencyManagement> emit content_entity rows with groupId:artifactId identity; local <properties> resolve at parse time, unresolved or parent-POM references stay marked partial/unresolved.",
		},
		{
			Ecosystem:               "gradle",
			FilePattern:             "build.gradle",
			FileKind:                "build",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/gradle/parser.go",
			Notes:                   "Groovy DSL dependencies blocks (implementation, api, runtimeOnly, compileOnly, testImplementation, platform/enforcedPlatform, buildscript) emit content_entity rows; project()/files()/fileTree() are skipped and unresolved interpolations stay marked unresolved.",
		},
		{
			Ecosystem:               "gradle",
			FilePattern:             "build.gradle.kts",
			FileKind:                "build",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/gradle/parser.go",
			Notes:                   "Kotlin DSL dependencies blocks emit content_entity rows mirroring the Groovy parser; configuration closures (e.g., version { strictly(...) }) without a top-level coordinate version remain partial.",
		},
		{
			Ecosystem:               "nuget",
			FilePattern:             "*.csproj",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/nuget_project_language.go",
			Notes:                   "PackageReference rows preserve requested versions, resolved MSBuild properties, unresolved-property partial evidence, and PrivateAssets dev/test signals.",
		},
		{
			Ecosystem:               "nuget",
			FilePattern:             "packages.lock.json",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/json/nuget_lock.go",
			Notes:                   "NuGet lockfiles emit exact resolved versions and direct/transitive dependency paths when the lockfile proves them.",
		},
		{
			Ecosystem:               "rubygems",
			FilePattern:             "gemfile",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/ruby/bundler_gemfile.go",
			Notes:                   "Literal gem declarations emit RubyGems dependency rows with group and git/path source metadata; dynamic Ruby is intentionally skipped.",
		},
		{
			Ecosystem:               "rubygems",
			FilePattern:             "gemfile.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/ruby/bundler_lockfile.go",
			Notes:                   "Bundler lockfiles emit exact versions and dependency_path/depth only when DEPENDENCIES and specs indentation prove the chain.",
		},
		{
			Ecosystem:               "cargo",
			FilePattern:             "cargo.toml",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/rust/cargo_dependencies.go",
			Notes:                   "Cargo manifests emit dependency rows for direct, dev, build, target-specific, workspace-inherited, and renamed package dependencies; transitive chains require Cargo.lock.",
		},
		{
			Ecosystem:               "cargo",
			FilePattern:             "cargo.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/rust/cargo_dependencies.go",
			Notes:                   "Cargo lockfiles emit exact crate versions and dependency paths only when the lock graph proves a package is reachable from a workspace root.",
		},
		{
			Ecosystem:               "hex",
			FilePattern:             "mix.exs",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/elixir/hex_dependencies.go",
			Notes:                   "Literal Mix deps emit Hex dependency rows with runtime/dev/test scope and organization namespace when present; git dependencies surface as vcs_dependency provenance so registry consumption is not invented.",
		},
		{
			Ecosystem:               "hex",
			FilePattern:             "mix.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/elixir/hex_dependencies.go",
			Notes:                   "Hex lockfile rows emit exact top-level package versions and bounded one-hop dependency paths when mix.lock records Hex dependencies; non-Hex lock entries stay out of consumption truth.",
		},
		{
			Ecosystem:               "swift",
			FilePattern:             "Package.resolved",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			SourceReference:         "go/internal/parser/json/swift_package_resolved.go",
			Notes:                   "Swift Package.resolved v2 remote source-control pins emit exact-version rows with source namespace and SwiftPM identity; branch, revision-only, local, path, and unsupported pins remain non-evidence.",
		},
		{
			Ecosystem:               "pub",
			FilePattern:             "pubspec.yaml",
			FileKind:                "manifest",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesVersionRange:    true,
			CapturesScope:           true,
			CapturesDevRuntimeSplit: true,
			SourceReference:         "go/internal/parser/yaml/pubspec.go",
			Notes:                   "Pub manifests emit hosted dependency rows for dependencies and dev_dependencies; git/path dependencies and dependency_overrides remain non-evidence.",
		},
		{
			Ecosystem:               "pub",
			FilePattern:             "pubspec.lock",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			SourceReference:         "go/internal/parser/yaml/pubspec.go",
			Notes:                   "Pub lockfiles emit exact hosted pub.dev versions with direct/transitive and runtime/dev scope evidence; git, private hosted, and mismatched package rows remain non-evidence.",
		},
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Ecosystem != entries[j].Ecosystem {
			return entries[i].Ecosystem < entries[j].Ecosystem
		}
		if entries[i].FileKind != entries[j].FileKind {
			return entries[i].FileKind < entries[j].FileKind
		}
		return entries[i].FilePattern < entries[j].FilePattern
	})
	return entries
}

// DependencyCoverageByFile returns the coverage entry for a lowercase
// filename, allowing collector and reducer code paths to assert that a
// specific manifest is covered before claiming repository dependency
// evidence. The lookup is case-insensitive and matches suffix wildcard
// patterns such as `*.csproj` used for project manifests.
func DependencyCoverageByFile(filename string) (DependencyCoverageEntry, bool) {
	target := strings.ToLower(strings.TrimSpace(filename))
	if target == "" {
		return DependencyCoverageEntry{}, false
	}
	for _, entry := range DependencyCoverage() {
		if dependencyCoveragePatternMatches(entry.FilePattern, target) {
			return entry, true
		}
	}
	return DependencyCoverageEntry{}, false
}

func dependencyCoveragePatternMatches(pattern string, target string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == target {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(target, strings.TrimPrefix(pattern, "*"))
	}
	return false
}
