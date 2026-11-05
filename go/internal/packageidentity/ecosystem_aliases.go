// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "strings"

// ecosystemAliasTable is the one source of ecosystem spellings: every
// lower-case, trimmed spelling NormalizeEcosystem accepts, grouped by the
// canonical ecosystem it maps to. NormalizeEcosystem and EcosystemAliases both
// read it, so a SQL prefilter bound from EcosystemAliases matches exactly the
// rows the Go normalizer would admit (#9874).
var ecosystemAliasTable = []struct {
	canonical Ecosystem
	aliases   []string
}{
	{EcosystemNPM, []string{"npm", "node", "nodejs", "javascript", "typescript"}},
	{EcosystemPyPI, []string{"pypi", "pip", "python"}},
	{EcosystemGoModule, []string{"go", "golang", "gomod", "go-module", "go_module"}},
	{EcosystemMaven, []string{"maven", "gradle", "java"}},
	{EcosystemNuGet, []string{"nuget", "dotnet", ".net"}},
	{EcosystemComposer, []string{"composer", "packagist", "php"}},
	{EcosystemRubyGems, []string{"rubygems", "gem", "ruby"}},
	{EcosystemCargo, []string{"cargo", "crate", "crates", "crates.io", "rust"}},
	{EcosystemSwift, []string{"swift", "swifturl", "swiftpm", "spm", "swift-package-manager"}},
	{EcosystemHex, []string{"hex", "hexpm", "hex.pm"}},
	{EcosystemPub, []string{"pub", "pub.dev", "dart", "dart-pub"}},
	{EcosystemOS, []string{
		"os", "apk", "alpine", "deb", "debian", "rpm", "rhel", "ubuntu",
		"alma", "almalinux", "rocky", "rockylinux", "rocky-linux", "rocky linux",
	}},
	{EcosystemGeneric, []string{"generic"}},
}

// ecosystemBySpelling is ecosystemAliasTable keyed by spelling.
var ecosystemBySpelling = func() map[string]Ecosystem {
	out := map[string]Ecosystem{}
	for _, row := range ecosystemAliasTable {
		for _, alias := range row.aliases {
			out[alias] = row.canonical
		}
	}
	return out
}()

// NormalizeEcosystem converts package-manager aliases into Eshu's canonical
// ecosystem names. An unknown spelling returns "" so callers fail closed.
func NormalizeEcosystem(value Ecosystem) Ecosystem {
	return ecosystemBySpelling[LowerASCII(strings.TrimSpace(string(value)))]
}

// EcosystemAliases returns every lower-case spelling NormalizeEcosystem maps to
// the same canonical ecosystem as value, canonical name included, or nil for
// an unknown ecosystem. SQL that prefilters a stored ecosystem by a canonical
// name binds this list against the folded column, because sources store raw
// spellings (a GitLab Gemnasium slug stores go, packagist, gem).
func EcosystemAliases(value Ecosystem) []string {
	canonical := NormalizeEcosystem(value)
	if canonical == "" {
		return nil
	}
	for _, row := range ecosystemAliasTable {
		if row.canonical == canonical {
			return append([]string(nil), row.aliases...)
		}
	}
	return nil
}
