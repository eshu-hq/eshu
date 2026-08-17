// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const rationaleDeltaCassettePath = "../../../testdata/cassettes/rationale/ifa-rationale-family-delta.json"

func TestRationaleDeltaCassetteMatchesNativePythonCollection(t *testing.T) {
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine: %v", err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repo-rationale")
	for relativePath, source := range rationaleDeltaCassettePythonSources() {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", relativePath, err)
		}
		if err := os.WriteFile(absolutePath, []byte(source), 0o600); err != nil {
			t.Fatalf("write %q: %v", relativePath, err)
		}
	}

	observedAt := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	chargePath := filepath.Join(repoRoot, "services", "payments", "charge.py")
	snapshot, err := (NativeRepositorySnapshotter{
		Engine: engine, ParseWorkers: 1, Now: func() time.Time { return observedAt },
	}).SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath: repoRoot, RemoteURL: rationaleCassetteRemoteURL,
		FileTargets: []string{chargePath}, Delta: true,
	})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	if got, want := snapshot.DeltaRelativePaths, []string{"services/payments/charge.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delta paths = %#v, want %#v", got, want)
	}
	repo, err := repositoryidentity.MetadataFor("repo-rationale", snapshot.RepoPath, rationaleCassetteRemoteURL)
	if err != nil {
		t.Fatalf("repositoryidentity.MetadataFor: %v", err)
	}
	if repo.ID != rationaleCassetteRepoID {
		t.Fatalf("fixed remote repository id = %q, want %q", repo.ID, rationaleCassetteRepoID)
	}

	generation := buildStreamingGeneration(
		snapshot.RepoPath,
		repo,
		"run-ifa-rationale-family-1",
		observedAt,
		snapshot,
		false,
		"",
	)
	got := make([]rationaleCassetteFact, 0, 4)
	for _, envelope := range drainCollectorFacts(t, generation) {
		if !rationaleCassetteSelectedFact(envelope.FactKind, envelope.StableFactKey) {
			continue
		}
		got = append(got, rationaleCassetteFact{
			FactKind: envelope.FactKind, StableFactKey: envelope.StableFactKey,
			SchemaVersion: "1.0.0", CollectorKind: envelope.CollectorKind,
			SourceConfidence: envelope.SourceConfidence,
			Payload:          normalizeRationaleCassettePayload(envelope.Payload, snapshot.RepoPath),
		})
	}
	if len(got) != 4 {
		t.Fatalf("native rationale delta facts = %d, want exact 4", len(got))
	}
	want := loadRationaleDeltaCassetteFacts(t, got)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("native rationale delta facts drifted from cassette\nnative: %s\ncassette: %s", gotJSON, wantJSON)
	}
}

func rationaleDeltaCassettePythonSources() map[string]string {
	sources := rationaleCassettePythonSources()
	sources["services/payments/charge.py"] = "# Retry policy for payment attempts.\n" +
		"# Maximum attempts are enforced by the caller.\n" +
		"# This function performs one charge.\n" +
		"# No rationale marker remains.\n" +
		"def charge():\n    pass\n"
	return sources
}

func loadRationaleDeltaCassetteFacts(t *testing.T, native []rationaleCassetteFact) []rationaleCassetteFact {
	t.Helper()
	raw, err := os.ReadFile(rationaleDeltaCassettePath)
	if err != nil {
		nativeJSON, _ := json.MarshalIndent(native, "", "  ")
		t.Fatalf("read rationale delta cassette: %v\nnative fixture candidate: %s", err, nativeJSON)
	}
	var file struct {
		Scopes []struct {
			Facts []rationaleCassetteFact `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode rationale delta cassette: %v", err)
	}
	if len(file.Scopes) != 1 {
		t.Fatalf("rationale delta cassette scopes = %d, want 1", len(file.Scopes))
	}
	return file.Scopes[0].Facts
}
