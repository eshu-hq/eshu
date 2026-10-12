// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPilotContractFileRejectsSymlinkEscape(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			base, outside := t.TempDir(), t.TempDir()
			writePilotLoaderFixture(t, filepath.Join(outside, "contract.yaml"), "version: 1\nrationale: outside\n")
			if directory {
				if err := os.Symlink(outside, filepath.Join(base, "contracts")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(base, "contracts"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "contract.yaml"), filepath.Join(base, "contracts", "contract.yaml")); err != nil {
					t.Fatal(err)
				}
			}
			manifest := filepath.Join(base, "manifest.yaml")
			writePilotLoaderFixture(t, manifest, "version: 1\nentries:\n  - id: probe\n    contract_file: contracts/contract.yaml\n")
			if loaded, err := LoadManifestFile(manifest); err == nil {
				t.Fatalf("accepted outside contract through %s symlink: %+v", name, loaded.Entries[0].Contract)
			}
		})
	}
}

func TestPilotContractLoadingRejectsUnknownFields(t *testing.T) {
	for _, field := range []string{"required_schmea: [required_index]", "require_oder: true", "budget:\n  max_quer_count: 3"} {
		for _, inline := range []bool{false, true} {
			name := strings.Split(field, ":")[0]
			if inline {
				name += "/inline"
			} else {
				name += "/file"
			}
			t.Run(name, func(t *testing.T) {
				base := t.TempDir()
				contract := "version: 1\n" + field + "\n"
				manifest := "version: 1\nentries:\n  - id: probe\n"
				if inline {
					manifest += "    contract:\n      " + strings.ReplaceAll(strings.TrimSuffix(contract, "\n"), "\n", "\n      ") + "\n"
				} else {
					manifest += "    contract_file: contracts/contract.yaml\n"
					writePilotLoaderFixture(t, filepath.Join(base, "contracts", "contract.yaml"), contract)
				}
				path := filepath.Join(base, "manifest.yaml")
				writePilotLoaderFixture(t, path, manifest)
				if loaded, err := LoadManifestFile(path); err == nil {
					t.Fatalf("accepted unknown contract field %q: %+v", field, loaded.Entries[0].Contract)
				}
			})
		}
	}
}

func TestPilotContractFileRejectsExtraDocument(t *testing.T) {
	base := t.TempDir()
	writePilotLoaderFixture(t, filepath.Join(base, "contracts", "contract.yaml"), "version: 1\n---\nrequire_order: true\n")
	path := filepath.Join(base, "manifest.yaml")
	writePilotLoaderFixture(t, path, "version: 1\nentries:\n  - id: probe\n    contract_file: contracts/contract.yaml\n")
	if _, err := LoadManifestFile(path); err == nil {
		t.Fatal("accepted multiple contract documents")
	}
}

func TestPilotContractFileAcceptsContainedSymlink(t *testing.T) {
	base := t.TempDir()
	writePilotLoaderFixture(t, filepath.Join(base, "contracts", "nested", "contract.yaml"), "version: 1\nrequire_order: true\n")
	if err := os.Symlink("nested/contract.yaml", filepath.Join(base, "contracts", "contract.yaml")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(base, "manifest.yaml")
	writePilotLoaderFixture(t, manifest, "version: 1\nentries:\n  - id: probe\n    contract_file: contracts/contract.yaml\n")
	loaded, err := LoadManifestFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Entries[0].Contract.RequireOrder {
		t.Fatal("lost contract guard through contained symlink")
	}
}

func TestPilotContractLoadingPreservesKnownGuards(t *testing.T) {
	for _, inline := range []bool{false, true} {
		name := "file"
		if inline {
			name = "inline"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			contract := "version: 1\nrequire_order: true\nrequire_limit: true\nrequired_schema: [required_index]\n"
			manifest := "version: 1\nentries:\n  - id: probe\n"
			if inline {
				manifest += "    contract:\n      " + strings.ReplaceAll(strings.TrimSuffix(contract, "\n"), "\n", "\n      ") + "\n"
			} else {
				manifest += "    contract_file: contracts/nested/contract.yaml\n"
				writePilotLoaderFixture(t, filepath.Join(base, "contracts", "nested", "contract.yaml"), contract)
			}
			path := filepath.Join(base, "manifest.yaml")
			writePilotLoaderFixture(t, path, manifest)
			loaded, err := LoadManifestFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := loaded.Entries[0].Contract
			if got == nil || !got.RequireOrder || !got.RequireLimit || len(got.RequiredSchema) != 1 || got.RequiredSchema[0] != "required_index" {
				t.Fatalf("lost known contract guards: %+v", got)
			}
		})
	}
}

func writePilotLoaderFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
