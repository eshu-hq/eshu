// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestFilesystemCopyDropsPolicyGovernedDotfiles proves the managed-workspace
// copy does not carry dotfiles the content-store policy withholds or redacts
// (#8904). A name the registry binds under metadata (.npmrc) or redact
// (.yarnrc.yml) must not reach discovery through the filesystem copy, and
// .gitignore stays out because the copy applies the source ignore rules
// itself. Unadmitted credential names (.env) stay pruned as before.
func TestFilesystemCopyDropsPolicyGovernedDotfiles(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".editorconfig"), "root = true\n")
	dropped := []string{".gitignore", ".npmrc", ".yarnrc.yml", ".env"}
	for _, rel := range dropped {
		writeSelectionTestFile(t, filepath.Join(sourceRepo, rel), "x = 1\n")
	}

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	copied := filepath.Join(reposDir, "eshu-hq", "service-a")
	for _, rel := range dropped {
		if _, err := os.Stat(filepath.Join(copied, rel)); err == nil {
			t.Errorf("managed copy kept %q, want it pruned", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(copied, ".editorconfig")); err != nil {
		t.Errorf("managed copy missing admitted control %q: %v", ".editorconfig", err)
	}
}

// TestRegistryDerivedHiddenKeepSetNeedsNoSecondEdit proves the filesystem copy
// derives its kept dotfiles from the parser registry (#8904): a dotfile name
// added to the registry is kept with no second edit. The probe registry binds
// a novel exact name nothing else admits; the live registry pins the
// exclusions (the ignore-control file and the metadata/redact policies) and
// the automatic keep of exact and extension-admitted store names.
func TestRegistryDerivedHiddenKeepSetNeedsNoSecondEdit(t *testing.T) {
	t.Parallel()

	definitions := append([]parser.Definition{}, parser.DefaultRegistry().Definitions()...)
	definitions = append(definitions, parser.Definition{
		ParserKey:  "test_newtool",
		Language:   "test_newtool",
		Admission:  parser.AdmissionContentText,
		ExactNames: []string{".newtoolrc"},
	})
	extended, err := parser.NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v, want nil", err)
	}
	for _, rel := range []string{".newtoolrc", "nested/dir/.newtoolrc"} {
		if !registryKeepsHiddenPath(extended, rel) {
			t.Errorf("registryKeepsHiddenPath(%q) = false, want true: a registry dotfile needs no second edit", rel)
		}
	}

	live := parser.DefaultRegistry()
	for _, rel := range []string{".gitignore", ".npmrc", ".yarnrc.yml", ".env", ".pypirc", ".auto.tfvars"} {
		if registryKeepsHiddenPath(live, rel) {
			t.Errorf("registryKeepsHiddenPath(%q) = true, want false", rel)
		}
	}
	for _, rel := range []string{".editorconfig", ".nvmrc", ".flake8", ".gitlab-ci.yml", "nested/.gitlab-ci.yaml"} {
		if !registryKeepsHiddenPath(live, rel) {
			t.Errorf("registryKeepsHiddenPath(%q) = false, want true", rel)
		}
	}
}
