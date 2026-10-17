// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"reflect"
	"testing"
)

// TestStoredRowsCopiesTheBuildVerbatim pins storedRows as a verbatim
// projection of the name-free build: path, relative_path and evidence_kind
// are copied byte for byte, in input order, and NameScoped is the name-scope
// table's value for the kind. The extractor-version guard in
// content/artifacts (TestExtractorOutputIsPinned) hashes its own copy of this
// mapping (deriveGuardRows in version_digest_test.go), so a change here is
// invisible to it. This test is the tripwire: a change that makes it fail
// moves stored rows, so it needs an ExtractorVersion bump and the same change
// to the guard's copy. The inputs hold upper case, a space, non-ASCII, a
// leading "./", a doubled slash and a dot segment. The S4 live test cannot
// see a case change: its corpus holds no row with an upper-case
// relative_path.
func TestStoredRowsCopiesTheBuildVerbatim(t *testing.T) {
	built := []map[string]any{
		{"path": "./Deploy/Secrets//DB Password.TXT", "source_repo": "", "relative_path": "Compose/Docker-Compose.YML", "evidence_kind": "docker_compose_secret_file"},
		{"path": "inventory/Hosts.ini", "relative_path": "Playbooks/../Site.yml", "evidence_kind": "ansible_inventory"},
		{"path": "modules/réseau", "source_repo": nil, "relative_path": "live/Prod/terragrunt.hcl", "evidence_kind": "terragrunt_dependency_config_path"},
	}
	want := []Row{
		{RawPath: "./Deploy/Secrets//DB Password.TXT", RelativePath: "Compose/Docker-Compose.YML", EvidenceKind: "docker_compose_secret_file", NameScoped: true},
		{RawPath: "inventory/Hosts.ini", RelativePath: "Playbooks/../Site.yml", EvidenceKind: "ansible_inventory", NameScoped: false},
		{RawPath: "modules/réseau", RelativePath: "live/Prod/terragrunt.hcl", EvidenceKind: "terragrunt_dependency_config_path", NameScoped: true},
	}
	got, err := storedRows(built)
	if err != nil {
		t.Fatalf("storedRows() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("storedRows() = %+v, want %+v; storedRows must copy the build verbatim, and a change to it needs an ExtractorVersion bump", got, want)
	}
}
