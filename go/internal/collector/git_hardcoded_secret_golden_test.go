// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestDeployableConfigHardcodedSecretFixtureReachesContentFact(t *testing.T) {
	t.Parallel()

	const (
		relativePath = "config/runtime.cfg"
		fixtureBody  = "password = \"invalid-by-design\"\n"
		remoteURL    = "https://github.com/acme/deployable-config"
	)
	fixtureRoot := filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "deployable-config")
	fixturePath := filepath.Join(fixtureRoot, filepath.FromSlash(relativePath))
	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read committed hardcoded-secret fixture: %v", err)
	}
	if got := string(body); got != fixtureBody {
		t.Fatalf("fixture body = %q, want %q", got, fixtureBody)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:        fixtureRoot,
			RemoteURL:       remoteURL,
			SourceCommitSHA: "golden-hardcoded-secret-fixture",
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	var matchingMetas []ContentFileMeta
	for _, meta := range snapshot.ContentFileMetas {
		if meta.RelativePath == relativePath {
			matchingMetas = append(matchingMetas, meta)
		}
	}
	if got, want := len(matchingMetas), 1; got != want {
		t.Fatalf("content metadata for %q = %d, want %d", relativePath, got, want)
	}
	if got, want := matchingMetas[0].Language, "config"; got != want {
		t.Fatalf("content metadata language = %q, want %q", got, want)
	}
	if matchingMetas[0].Digest == "" {
		t.Fatal("content metadata digest = empty, want content hash")
	}

	repo, err := repositoryidentity.MetadataFor("deployable-config", snapshot.RepoPath, remoteURL)
	if err != nil {
		t.Fatalf("repositoryidentity.MetadataFor() error = %v, want nil", err)
	}
	if got, want := repo.ID, "repository:r_217415d9"; got != want {
		t.Fatalf("fixture repository ID = %q, want %q", got, want)
	}
	observedAt := time.Date(2026, time.August, 8, 16, 0, 0, 0, time.UTC)
	collected := buildStreamingGeneration(
		snapshot.RepoPath,
		repo,
		"golden-hardcoded-secret-run",
		observedAt,
		snapshot,
		false,
		"",
	)

	var matchingFacts int
	for _, envelope := range drainFactChannel(collected.Facts) {
		if envelope.FactKind != "content" || envelope.Payload["content_path"] != relativePath {
			continue
		}
		matchingFacts++
		for field, want := range map[string]string{
			"content_path": relativePath,
			"content_body": fixtureBody,
			"language":     "config",
			"repo_id":      "repository:r_217415d9",
		} {
			if got, _ := envelope.Payload[field].(string); got != want {
				t.Errorf("content fact payload[%q] = %q, want %q", field, got, want)
			}
		}
		if got, want := envelope.ScopeID, "git-repository-scope:repository:r_217415d9"; got != want {
			t.Errorf("content fact ScopeID = %q, want %q", got, want)
		}
		if got, want := envelope.StableFactKey, "content:repository:r_217415d9:config/runtime.cfg"; got != want {
			t.Errorf("content fact StableFactKey = %q, want %q", got, want)
		}
	}
	if got, want := matchingFacts, 1; got != want {
		t.Fatalf("content facts for %q = %d, want %d", relativePath, got, want)
	}
}
