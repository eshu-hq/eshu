// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestFilesystemCopyPreservesMetadataOnlyHtaccess(t *testing.T) {
	t.Parallel()
	for _, path := range []string{".htaccess", "sites/site/.htaccess", "SITES/site/.HtAccess"} {
		if !preserveFilesystemHiddenPath(path) {
			t.Errorf("%q pruned", path)
		}
	}
	for _, path := range []string{".npmrc", "sites/site/.env"} {
		if preserveFilesystemHiddenPath(path) {
			t.Errorf("%q unexpectedly preserved", path)
		}
	}
}

func TestFilesystemCopiedHtaccessReachesFactsWithoutStoredBody(t *testing.T) {
	t.Parallel()
	const secret = "SeedHtaccessCredential987654321"
	root := t.TempDir()
	managed := t.TempDir()
	source := filepath.Join(root, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(source, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(source, ".htaccess"), "php_value memory_limit 256M\nphp_value password "+secret+"\n")
	selection, err := syncFilesystemRepositories(context.Background(), RepoSyncConfig{ReposDir: managed, FilesystemRoot: root}, []string{"eshu-hq/service-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.SelectedRepoPaths) != 1 {
		t.Fatalf("selected paths = %v", selection.SelectedRepoPaths)
	}
	copied := selection.SelectedRepoPaths[0]
	if _, err := os.Stat(filepath.Join(copied, ".htaccess")); err != nil {
		t.Fatalf(".htaccess did not reach parser workspace: %v", err)
	}
	registry := parser.DefaultRegistry()
	engine, err := parser.NewEngine(registry, parser.NewRuntime())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine, Registry: registry}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: copied})
	if err != nil {
		t.Fatal(err)
	}
	observed, _ := time.Parse(time.RFC3339, policyObservedAt)
	collected := buildStreamingGeneration(copied, repositoryidentity.Metadata{ID: "repo://example/php-copy", Name: "php-copy"}, "run", observed, snapshot, false, "")
	facts := drainFactChannel(collected.Facts)
	if body := contentFact(t, facts, ".htaccess").Payload["content_body"]; body != "" {
		t.Fatalf("content body = %#v", body)
	}
	assertNoSeedInAnyFact(t, facts, secret)
	found := false
	for _, fact := range facts {
		if fact.FactKind == "content_entity" && fact.Payload["relative_path"] == ".htaccess" && fact.Payload["entity_name"] == "php_directive:memory_limit" {
			found = true
			if fact.Payload["source_cache"] != "" {
				t.Fatalf("entity source cache = %#v", fact.Payload["source_cache"])
			}
		}
	}
	if !found {
		t.Fatal("safe .htaccess directive did not reach content facts")
	}
}
