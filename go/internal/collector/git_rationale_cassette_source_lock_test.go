// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	rationaleCassetteRepoID    = "repository:r_f781caa5"
	rationaleCassetteRepoPath  = "/repo-rationale"
	rationaleCassetteRemoteURL = "https://github.com/eshu-hq/ifa-rationale-fixture"
)

type rationaleCassetteFact struct {
	FactKind         string         `json:"fact_kind"`
	StableFactKey    string         `json:"stable_fact_key"`
	SchemaVersion    string         `json:"schema_version"`
	CollectorKind    string         `json:"collector_kind"`
	SourceConfidence string         `json:"source_confidence"`
	Payload          map[string]any `json:"payload"`
}

func TestRationaleCassetteMatchesNativePythonCollection(t *testing.T) {
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine: %v", err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repo-rationale")
	for relativePath, source := range rationaleCassettePythonSources() {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", relativePath, err)
		}
		if err := os.WriteFile(absolutePath, []byte(source), 0o600); err != nil {
			t.Fatalf("write %q: %v", relativePath, err)
		}
	}
	observedAt := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{
		Engine: engine, ParseWorkers: 1, Now: func() time.Time { return observedAt },
	}).SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath: repoRoot, RemoteURL: rationaleCassetteRemoteURL,
	})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	repo, err := repositoryidentity.MetadataFor("repo-rationale", snapshot.RepoPath, rationaleCassetteRemoteURL)
	if err != nil {
		t.Fatalf("repositoryidentity.MetadataFor: %v", err)
	}
	if repo.ID != rationaleCassetteRepoID {
		t.Fatalf("fixed remote repository id = %q, want literal lock %q", repo.ID, rationaleCassetteRepoID)
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
	got := make([]rationaleCassetteFact, 0, 12)
	counts := map[string]int{}
	for _, envelope := range drainCollectorFacts(t, generation) {
		if !rationaleCassetteSelectedFact(envelope.FactKind, envelope.StableFactKey) {
			continue
		}
		counts[envelope.FactKind]++
		got = append(got, rationaleCassetteFact{
			FactKind: envelope.FactKind, StableFactKey: envelope.StableFactKey,
			// Git facts leave SchemaVersion empty; cassette v1 requires a
			// declared replay wrapper version. Payload and identity stay exact.
			SchemaVersion: "1.0.0", CollectorKind: envelope.CollectorKind,
			SourceConfidence: envelope.SourceConfidence,
			Payload:          normalizeRationaleCassettePayload(envelope.Payload, snapshot.RepoPath),
		})
	}
	wantCounts := map[string]int{"repository": 1, "file": 5, "content_entity": 5, "shared_followup": 1}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("native rationale fact inventory = %#v, want %#v", counts, wantCounts)
	}
	want := loadRationaleCassetteFacts(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("native rationale facts drifted from cassette\nnative: %s\ncassette: %s", gotJSON, wantJSON)
	}
}

func rationaleCassettePythonSources() map[string]string {
	return map[string]string{
		"services/payments/charge.py":     "# WHY: Retries are capped at three to bound tail latency.\n# NOTE: Retries are capped at three to bound tail latency.\n# WHY: Retries are capped at three to bound tail latency.\n# TODO:\ndef charge():\n    pass\n",
		"services/billing/invoice.py":     "# HACK: Invoices are immutable once issued.\ndef issue_invoice():\n    pass\n",
		"services/payments/refund.py":     "# why: lowercase markers are ignored\n# CAVEAT: unsupported markers are ignored\ndef refund():\n    pass\n",
		"services/support/reconcile.py":   "# FIXME: detached by a blank line\n\ndef reconcile():\n    pass\n",
		"services/support/healthcheck.py": "def healthcheck():\n    pass\n",
	}
}

func rationaleCassetteSelectedFact(kind, stableFactKey string) bool {
	switch kind {
	case "repository", "file", "content_entity":
		return true
	case "shared_followup":
		return stableFactKey == "shared_followup:"+rationaleCassetteRepoID+":rationale_materialization"
	default:
		return false
	}
}

func normalizeRationaleCassettePayload(payload map[string]any, repoRoot string) map[string]any {
	raw, _ := json.Marshal(payload)
	normalized := strings.ReplaceAll(string(raw), filepath.ToSlash(repoRoot), rationaleCassetteRepoPath)
	var result map[string]any
	_ = json.Unmarshal([]byte(normalized), &result)
	return result
}

func loadRationaleCassetteFacts(t *testing.T) []rationaleCassetteFact {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "cassettes", "rationale", "ifa-rationale-family.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rationale cassette: %v", err)
	}
	var file struct {
		Scopes []struct {
			Facts []rationaleCassetteFact `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode rationale cassette: %v", err)
	}
	if len(file.Scopes) != 1 {
		t.Fatalf("rationale cassette scopes = %d, want 1", len(file.Scopes))
	}
	return file.Scopes[0].Facts
}
