// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Source kinds for the manifest files. Unlike a pin file, one manifest holds
// several declaring sections, and a reader needs the section to tell a
// constraint (engines) from a pin (volta, packageManager, the go directive), so
// the source kind names the section rather than the file. A Dockerfile has one
// declaring section (its FROM lines) and a workflow has one (its setup with:
// version inputs), so each has a single kind.
const (
	sourceKindPackageJSONEngines        = "package_json_engines"
	sourceKindPackageJSONVolta          = "package_json_volta"
	sourceKindPackageJSONManager        = "package_json_package_manager"
	sourceKindComposerRequire           = "composer_json_require_php"
	sourceKindComposerPlatform          = "composer_json_platform_php"
	sourceKindComposerLock              = "composer_lock_platform_php"
	sourceKindComposerOverrides         = "composer_lock_platform_overrides_php"
	sourceKindGoModGo                   = "go_mod_go"
	sourceKindGoModToolchain            = "go_mod_toolchain"
	sourceKindDockerfileFrom            = "dockerfile_from"
	sourceKindWorkflowSetup             = "workflow_setup_version"
	sourceKindPyProjectRequires         = "pyproject_requires_python"
	sourceKindPoetryPython              = "poetry_python"
	sourceKindPipfileVersion            = "pipfile_python_version"
	sourceKindPipfileFullVersion        = "pipfile_python_full_version"
	sourceKindSetupCFGRequires          = "setup_cfg_python_requires"
	sourceKindSetupPYRequires           = "setup_py_python_requires"
	sourceKindGemfileRuby               = "gemfile_ruby"
	sourceKindGemfileLockRuby           = "gemfile_lock_ruby_version"
	sourceKindCpanfilePerl              = "cpanfile_perl"
	sourceKindDotNetGlobalSDK           = "dotnet_global_json_sdk"
	sourceKindDotNetTargetFramework     = "dotnet_csproj_target_framework"
	sourceKindDotNetTargetFrameworks    = "dotnet_csproj_target_frameworks"
	sourceKindPomCompilerRelease        = "pom_maven_compiler_release"
	sourceKindPomCompilerSource         = "pom_maven_compiler_source"
	sourceKindPomCompilerTarget         = "pom_maven_compiler_target"
	sourceKindPomJavaVersion            = "pom_java_version"
	sourceKindGradleToolchain           = "gradle_java_toolchain"
	sourceKindGradleCompatibility       = "gradle_java_source_compatibility"
	sourceKindGradleTargetCompatibility = "gradle_java_target_compatibility"
	sourceKindGradleKotlinToolchain     = "gradle_kotlin_jvm_toolchain"
	sourceKindSbtScalaVersion           = "sbt_scala_version"
	sourceKindSbtBuildVersion           = "sbt_build_properties_version"
	sourceKindSwiftToolsVersion         = "swift_tools_version"
	sourceKindTerraformRequired         = "terraform_required_version"
	sourceKindTerragruntConstraint      = "terragrunt_version_constraint"
	sourceKindHelmKubeVersion           = "helm_chart_kube_version"
	sourceKindCargoRustVersion          = "cargo_package_rust_version"
	sourceKindMixExsElixir              = "mix_exs_elixir"
	sourceKindStackYamlResolver         = "stack_yaml_resolver"
	sourceKindDartPubspecSDK            = "dart_pubspec_environment_sdk"
	sourceKindDartFvmrcFlutter          = "dart_fvmrc_flutter"
	sourceKindCabalTestedWith           = "cabal_tested_with"
)

// section identifies the manifest section a declaration was read from.
type section uint8

const (
	sectionNone section = iota
	sectionPackageJSONEngines
	sectionPackageJSONVolta
	sectionPackageJSONManager
	sectionComposerRequire
	sectionComposerPlatform
	sectionComposerLock
	sectionComposerOverrides
	sectionGoModGo
	sectionGoModToolchain
	sectionDockerfileFrom
	sectionWorkflowSetup
	sectionPyProjectRequires
	sectionPoetryPython
	sectionPipfileVersion
	sectionPipfileFullVersion
	sectionSetupCFGRequires
	sectionSetupPYRequires
	sectionGemfileRuby
	sectionGemfileLockRuby
	sectionCpanfilePerl
	sectionDotNetGlobalSDK
	sectionDotNetTargetFramework
	sectionDotNetTargetFrameworks
	sectionPomCompilerRelease
	sectionPomCompilerSource
	sectionPomCompilerTarget
	sectionPomJavaVersion
	sectionGradleToolchain
	sectionGradleCompatibility
	sectionGradleTargetCompatibility
	sectionGradleKotlinToolchain
	sectionSbtScalaVersion
	sectionSbtBuildVersion
	sectionSwiftToolsVersion
	sectionTerraformRequired
	sectionTerragruntConstraint
	sectionHelmKubeVersion
	sectionCargoRustVersion
	sectionMixExsElixir
	sectionStackYamlResolver
	sectionDartPubspecSDK
	sectionDartFvmrcFlutter
	sectionCabalTestedWith
)

// sectionSourceKinds maps a section to its source kind.
var sectionSourceKinds = [...]string{
	sectionNone:                      "",
	sectionPackageJSONEngines:        sourceKindPackageJSONEngines,
	sectionPackageJSONVolta:          sourceKindPackageJSONVolta,
	sectionPackageJSONManager:        sourceKindPackageJSONManager,
	sectionComposerRequire:           sourceKindComposerRequire,
	sectionComposerPlatform:          sourceKindComposerPlatform,
	sectionComposerLock:              sourceKindComposerLock,
	sectionComposerOverrides:         sourceKindComposerOverrides,
	sectionGoModGo:                   sourceKindGoModGo,
	sectionGoModToolchain:            sourceKindGoModToolchain,
	sectionDockerfileFrom:            sourceKindDockerfileFrom,
	sectionWorkflowSetup:             sourceKindWorkflowSetup,
	sectionPyProjectRequires:         sourceKindPyProjectRequires,
	sectionPoetryPython:              sourceKindPoetryPython,
	sectionPipfileVersion:            sourceKindPipfileVersion,
	sectionPipfileFullVersion:        sourceKindPipfileFullVersion,
	sectionSetupCFGRequires:          sourceKindSetupCFGRequires,
	sectionSetupPYRequires:           sourceKindSetupPYRequires,
	sectionGemfileRuby:               sourceKindGemfileRuby,
	sectionGemfileLockRuby:           sourceKindGemfileLockRuby,
	sectionCpanfilePerl:              sourceKindCpanfilePerl,
	sectionDotNetGlobalSDK:           sourceKindDotNetGlobalSDK,
	sectionDotNetTargetFramework:     sourceKindDotNetTargetFramework,
	sectionDotNetTargetFrameworks:    sourceKindDotNetTargetFrameworks,
	sectionPomCompilerRelease:        sourceKindPomCompilerRelease,
	sectionPomCompilerSource:         sourceKindPomCompilerSource,
	sectionPomCompilerTarget:         sourceKindPomCompilerTarget,
	sectionPomJavaVersion:            sourceKindPomJavaVersion,
	sectionGradleToolchain:           sourceKindGradleToolchain,
	sectionGradleCompatibility:       sourceKindGradleCompatibility,
	sectionGradleTargetCompatibility: sourceKindGradleTargetCompatibility,
	sectionGradleKotlinToolchain:     sourceKindGradleKotlinToolchain,
	sectionSbtScalaVersion:           sourceKindSbtScalaVersion,
	sectionSbtBuildVersion:           sourceKindSbtBuildVersion,
	sectionSwiftToolsVersion:         sourceKindSwiftToolsVersion,
	sectionTerraformRequired:         sourceKindTerraformRequired,
	sectionTerragruntConstraint:      sourceKindTerragruntConstraint,
	sectionHelmKubeVersion:           sourceKindHelmKubeVersion,
	sectionCargoRustVersion:          sourceKindCargoRustVersion,
	sectionMixExsElixir:              sourceKindMixExsElixir,
	sectionStackYamlResolver:         sourceKindStackYamlResolver,
	sectionDartPubspecSDK:            sourceKindDartPubspecSDK,
	sectionDartFvmrcFlutter:          sourceKindDartFvmrcFlutter,
	sectionCabalTestedWith:           sourceKindCabalTestedWith,
}

// MaxManifestBytesPerFile bounds one package.json, Composer, or go.mod body. A manifest
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
	manifestComposerJSON
	manifestComposerLock
	manifestGoMod
	manifestDockerfile
	manifestWorkflow
	manifestPythonTOML
	manifestSetupCFG
	manifestSetupPY
	manifestGemfile
	manifestGemfileLock
	manifestCpanfile
	manifestDotNetGlobal
	manifestDotNetProject
	manifestPom
	manifestGradle
	manifestSbt
	manifestSbtBuildProperties
	manifestSwiftPackage
	manifestTerraform
	manifestTerragrunt
	manifestHelmChart
	manifestCargo
	manifestMixExs
	manifestStackYaml
	manifestDartPubspec
	manifestDartFvmrc
	manifestHaskellCabal
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
// the exact base name package.json or go.mod at any depth, a Dockerfile at
// any depth, a GitHub Actions workflow file, or a Terraform, Terragrunt, or
// Helm chart file, each outside a dependency directory. The package.json and go.mod match is case-sensitive, as npm and
// the go tool are; Dockerfiles match case-insensitively, as the parser
// registry does.
func manifestKindForPath(relativePath string) manifestKind {
	return manifestKindForBase(path.Base(relativePath), relativePath)
}

// manifestKindForBase is manifestKindForPath with the base name already
// computed, so a caller that also needs the pin check pays for it once.
func manifestKindForBase(base, relativePath string) manifestKind {
	var kind manifestKind
	switch {
	case base == "package.json":
		kind = manifestPackageJSON
	case base == "composer.json":
		kind = manifestComposerJSON
	case base == "composer.lock":
		kind = manifestComposerLock
	case base == "go.mod":
		kind = manifestGoMod
	case base == "Cargo.toml":
		kind = manifestCargo
	case base == "mix.exs":
		kind = manifestMixExs
	case base == "stack.yaml":
		kind = manifestStackYaml
	case strings.EqualFold(path.Ext(base), ".cabal"):
		kind = manifestHaskellCabal
	case base == "pyproject.toml" || base == "Pipfile":
		kind = manifestPythonTOML
	case base == "setup.cfg":
		kind = manifestSetupCFG
	case base == "setup.py":
		kind = manifestSetupPY
	case base == "Gemfile":
		kind = manifestGemfile
	case base == "Gemfile.lock":
		kind = manifestGemfileLock
	case base == "cpanfile":
		kind = manifestCpanfile
	case base == "global.json":
		kind = manifestDotNetGlobal
	case path.Ext(base) == ".csproj":
		kind = manifestDotNetProject
	case base == "pom.xml":
		kind = manifestPom
	case base == "build.gradle" || base == "build.gradle.kts":
		kind = manifestGradle
	case base == "build.sbt":
		kind = manifestSbt
	case base == "Package.swift":
		kind = manifestSwiftPackage
	case base == "build.properties" && (relativePath == "project/build.properties" || strings.HasSuffix(relativePath, "/project/build.properties")):
		kind = manifestSbtBuildProperties
	case strings.HasSuffix(base, ".tf"):
		kind = manifestTerraform
	case base == "terragrunt.hcl":
		kind = manifestTerragrunt
	case strings.EqualFold(base, "Chart.yaml") || strings.EqualFold(base, "Chart.yml"):
		kind = manifestHelmChart
	case base == "pubspec.yaml" || base == "pubspec.yml":
		kind = manifestDartPubspec
	case base == ".fvmrc":
		kind = manifestDartFvmrc
	case isDockerfileName(base):
		kind = manifestDockerfile
	case ghactionsref.IsWorkflowPath(relativePath):
		// The workflow-path shape is exactly .github/workflows/<name>.yml,
		// so it can never sit under a dependency directory.
		return manifestWorkflow
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

// isDockerfileName reports whether a base name is a Dockerfile: the exact
// name or a suffixed variant, case-insensitively. It mirrors the parser
// registry's Dockerfile exact and prefix names, which the registry itself
// matches case-insensitively.
func isDockerfileName(base string) bool {
	lower := strings.ToLower(base)
	return lower == "dockerfile" || strings.HasPrefix(lower, "dockerfile.")
}

// FileClass says how this package reads a repository file.
type FileClass int

const (
	// FileNotAdmitted is any file this package does not read.
	FileNotAdmitted FileClass = iota
	// FilePin is a version-pin file; its body is parsed at emission.
	FilePin
	// FileManifest is a package.json, go.mod, Dockerfile, workflow,
	// Terraform, Terragrunt, Helm chart, or Package.swift file outside a
	// dependency directory; the content walk reads it when it meets it and
	// keeps only its extraction.
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

// safeSpec reports whether raw may be emitted as a manifest raw_spec. The
// manifests are admitted with the store content policy because the named
// sections hold only versions, but the key set is the file's, not ours: a
// credential pasted into engines or packageManager must never leave the file
// in a fact. A value passes only when it is short, plain ASCII from the version
// and range grammar, holds no opaque run, and is not a URL. Everything else is
// dropped and counted, never truncated or masked, because a masked fact would
// still claim a declaration the file does not make.
func safeSpec(raw string) bool { return shared.SafeVersionSpec(raw) }
