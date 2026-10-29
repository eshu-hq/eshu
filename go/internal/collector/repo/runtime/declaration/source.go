// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "path"

// Source kinds for the version-pin files. One value per file name, snake case.
// rust-toolchain and rust-toolchain.toml share a kind.
const (
	sourceKindNvmrc             = "nvmrc"
	sourceKindPHPVersion        = "php_version"
	sourceKindNodeVersion       = "node_version"
	sourceKindPythonVersion     = "python_version"
	sourceKindRubyVersion       = "ruby_version"
	sourceKindJavaVersion       = "java_version"
	sourceKindGoVersion         = "go_version"
	sourceKindTerraformVersion  = "terraform_version"
	sourceKindTerragruntVersion = "terragrunt_version"
	sourceKindToolVersions      = "tool_versions"
	sourceKindSdkmanrc          = "sdkmanrc"
	sourceKindRustToolchain     = "rust_toolchain"
	sourceKindSwiftVersion      = "swift_version"
	sourceKindRuntimeTXT        = "runtime_txt"
)

// pinFileSourceKinds maps the exact base names admitted by the parser registry
// as version-pin files (go/internal/parser/registry_definitions.go) to their
// source kind. It is an explicit name list on purpose: .npmrc, .yarnrc.yml,
// .pypirc, .netrc and mise.toml can carry credentials or literal environment
// values and must never reach this emitter. TestPinFileSourceKindsCoverEveryRegistryVersionPin
// fails when the registry admits a raw_text name that is in neither this map
// nor the test's list of non-pin configuration files.
var pinFileSourceKinds = map[string]string{
	".nvmrc":              sourceKindNvmrc,
	".php-version":        sourceKindPHPVersion,
	".node-version":       sourceKindNodeVersion,
	".python-version":     sourceKindPythonVersion,
	".ruby-version":       sourceKindRubyVersion,
	".java-version":       sourceKindJavaVersion,
	".go-version":         sourceKindGoVersion,
	".terraform-version":  sourceKindTerraformVersion,
	".terragrunt-version": sourceKindTerragruntVersion,
	".tool-versions":      sourceKindToolVersions,
	".sdkmanrc":           sourceKindSdkmanrc,
	"rust-toolchain":      sourceKindRustToolchain,
	"rust-toolchain.toml": sourceKindRustToolchain,
	".swift-version":      sourceKindSwiftVersion,
	"runtime.txt":         sourceKindRuntimeTXT,
}

// SourceKindForPath returns the source kind of a repository-relative path when
// its base name is one of the admitted version-pin files, at any depth.
func SourceKindForPath(relativePath string) (string, bool) {
	kind, ok := pinFileSourceKinds[path.Base(relativePath)]
	return kind, ok
}

// ScopeDir returns the directory of relativePath in the contract's form: "."
// for the repository root, otherwise slash separated, cleaned, with no leading
// "./" and no trailing "/".
func ScopeDir(relativePath string) string {
	dir := path.Dir(path.Clean(relativePath))
	if dir == "" || dir == "/" {
		return "."
	}
	return dir
}
