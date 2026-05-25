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
			Notes:                   "dependencies and devDependencies emit content_entity rows; optional and peer scopes are not yet split.",
		},
		{
			Ecosystem:               "npm",
			FilePattern:             "package-lock.json",
			FileKind:                "lockfile",
			Status:                  DependencyCoverageCovered,
			CapturesPackageIdentity: true,
			CapturesExactVersion:    true,
			CapturesScope:           true,
			CapturesDependencyChain: true,
			SourceReference:         "go/internal/parser/json/package_lock.go",
			Notes:                   "lockfile v3 and v1 emit exact-version rows with dependency_path/depth and direct_dependency flag.",
		},
		{
			Ecosystem:       "npm",
			FilePattern:     "yarn.lock",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "no parser registered in go/internal/parser/registry.go",
			Notes:           "Yarn classic and Berry lockfiles are not yet parsed; vulnerability findings for yarn-only repos must be reported as evidence_incomplete.",
		},
		{
			Ecosystem:       "npm",
			FilePattern:     "pnpm-lock.yaml",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/yaml/language.go does not branch on pnpm-lock.yaml",
			Notes:           "pnpm lockfiles are not yet parsed; impact for pnpm-only repos stays evidence_incomplete.",
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
			SourceReference:         "go/internal/parser/json/composer_lock.go",
			Notes:                   "packages and packages-dev arrays emit exact-version rows with a lockfile flag so the reducer can join manifest ranges to installed versions without dropping the dev/runtime split.",
		},
		{
			Ecosystem:       "pypi",
			FilePattern:     "pyproject.toml",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "no TOML parser registered in go/internal/parser/registry.go",
			Notes:           "PEP 621/Poetry/Hatch manifests are not yet parsed; PyPI repo dependencies remain missing.",
		},
		{
			Ecosystem:       "pypi",
			FilePattern:     "requirements.txt",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "no raw-text dependency parser branches on requirements*.txt",
			Notes:           "pip-style requirement files are not yet parsed; PyPI repo dependencies remain missing.",
		},
		{
			Ecosystem:       "pypi",
			FilePattern:     "pipfile",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "no TOML parser registered in go/internal/parser/registry.go",
			Notes:           "Pipenv manifest is not yet parsed.",
		},
		{
			Ecosystem:       "pypi",
			FilePattern:     "pipfile.lock",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/json/language.go does not branch on Pipfile.lock",
			Notes:           "Pipenv lockfile is JSON but not yet branched into dependency rows.",
		},
		{
			Ecosystem:       "pypi",
			FilePattern:     "poetry.lock",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "no TOML parser registered in go/internal/parser/registry.go",
			Notes:           "Poetry lockfile is TOML and not yet parsed.",
		},
		{
			Ecosystem:       "go",
			FilePattern:     "go.mod",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/go_package_interface_prescan.go reads go.mod for package interface only",
			Notes:           "go.mod is read for package-interface prescan but does not emit content_entity dependency facts; Go repos cannot yet correlate to vulnerability advisories through manifest evidence.",
		},
		{
			Ecosystem:       "go",
			FilePattern:     "go.sum",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "no parser registered in go/internal/parser/registry.go",
			Notes:           "Module checksum file is not parsed; exact-version evidence for Go repos is missing.",
		},
		{
			Ecosystem:       "maven",
			FilePattern:     "pom.xml",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "no XML parser registered in go/internal/parser/registry.go",
			Notes:           "Maven POMs are not yet parsed; vulnerability impact for Maven repos relies on registry-side evidence only.",
		},
		{
			Ecosystem:       "gradle",
			FilePattern:     "build.gradle",
			FileKind:        "build",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/registry.go registers groovy by extension but does not extract dependency blocks",
			Notes:           "Groovy build scripts are parsed for syntax only; dependency blocks are not yet extracted.",
		},
		{
			Ecosystem:       "gradle",
			FilePattern:     "build.gradle.kts",
			FileKind:        "build",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/registry.go registers kotlin by extension but does not extract dependency blocks",
			Notes:           "Kotlin DSL build scripts are parsed for syntax only; dependency blocks are not yet extracted.",
		},
		{
			Ecosystem:       "nuget",
			FilePattern:     "*.csproj",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "no XML parser registered in go/internal/parser/registry.go",
			Notes:           "C# project files are not parsed; .NET vulnerability impact relies on registry-side evidence only.",
		},
		{
			Ecosystem:       "nuget",
			FilePattern:     "packages.lock.json",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/json/language.go does not branch on packages.lock.json",
			Notes:           "NuGet central-lockfile JSON is not yet parsed into dependency rows.",
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
			Ecosystem:       "cargo",
			FilePattern:     "cargo.toml",
			FileKind:        "manifest",
			Status:          DependencyCoverageGap,
			SourceReference: "go/internal/parser/rust/cargo_cfg.go parses cfg signals only",
			Notes:           "Cargo manifests are only scanned for cfg/feature signals; dependency tables are not yet emitted as content_entity facts.",
		},
		{
			Ecosystem:       "cargo",
			FilePattern:     "cargo.lock",
			FileKind:        "lockfile",
			Status:          DependencyCoverageGap,
			SourceReference: "no TOML parser registered in go/internal/parser/registry.go",
			Notes:           "Cargo lockfile is TOML and not yet parsed.",
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
// evidence. The lookup is case-insensitive and does not match wildcard
// `*.csproj` style patterns.
func DependencyCoverageByFile(filename string) (DependencyCoverageEntry, bool) {
	target := strings.ToLower(strings.TrimSpace(filename))
	if target == "" {
		return DependencyCoverageEntry{}, false
	}
	for _, entry := range DependencyCoverage() {
		if entry.FilePattern == target {
			return entry, true
		}
	}
	return DependencyCoverageEntry{}, false
}
