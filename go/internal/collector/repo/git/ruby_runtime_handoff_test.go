// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/ruby"
)

func TestRubyRuntimeNativeHandoffAndPrivateTransport(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "Gemfile"), "source 'https://rubygems.org'\nruby '>= 3.2', '< 4.0'\ngem 'rack'\n")
	writeCollectorTestFile(t, filepath.Join(root, "Gemfile.lock"), "GEM\n  specs:\n    rack (3.0.0)\n\nRUBY VERSION\n   ruby 3.3.1p55\n")
	observed := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.RubyRuntime) != 2 {
		t.Fatalf("Ruby handoffs = %d", len(snapshot.RubyRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["ruby_runtime_evidence"]; ok {
			t.Fatal("private evidence in FileData")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-ruby", observed, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	declarations := decodeDeclarationList(t, envelopes)
	want := map[string]struct {
		value, path, kind string
		line              int
	}{
		"gemfile_ruby":              {">= 3.2, < 4.0", "Gemfile", "range", 2},
		"gemfile_lock_ruby_version": {"ruby 3.3.1p55", "Gemfile.lock", "exact", 6},
	}
	if len(declarations) != 2 {
		t.Fatalf("declarations = %+v", declarations)
	}
	for _, got := range declarations {
		expected, ok := want[got.SourceKind]
		if !ok || expected.value != got.RawSpec || expected.path != got.RelativePath || expected.kind != got.SpecKind || expected.line != got.LineNumber || got.OrderIndex != 0 || got.Runtime != "ruby" {
			t.Errorf("declaration = %+v", got)
		}
		delete(want, got.SourceKind)
	}
	if len(want) != 0 {
		t.Errorf("missing: %v", want)
	}
	for _, e := range factsByKind(envelopes, facts.RuntimeDeclarationVersionFactKind) {
		var row struct {
			RepoID       string `json:"repo_id"`
			RelativePath string `json:"relative_path"`
			LineNumber   int    `json:"line_number"`
			OrderIndex   int    `json:"order_index"`
		}
		body, err := json.Marshal(e.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &row); err != nil {
			t.Fatal(err)
		}
		wantKey := facts.StableID(facts.RuntimeDeclarationVersionFactKind, map[string]any{"repo_id": row.RepoID, "relative_path": row.RelativePath, "line_number": row.LineNumber, "runtime": "ruby", "order_index": row.OrderIndex})
		if e.StableFactKey != wantKey {
			t.Fatalf("stable key = %s, want %s", e.StableFactKey, wantKey)
		}
	}
	for _, e := range envelopes {
		body, err := json.Marshal(e.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "ruby_runtime_evidence") {
			t.Fatal("private evidence persisted")
		}
	}
}

func TestRubyRuntimeRawDigestAndGuard(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Gemfile")
	writeCollectorTestFile(t, path, "ruby '3.3.1'\r\n")
	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.RubyRuntime["Gemfile"].Digest; got != ruby.RubyRuntimeDigest([]byte("ruby '3.3.1'\r\n")) {
		t.Fatalf("digest = %q", got)
	}
}

func TestRubyRuntimeNativeRejectsNestedGemfileDirectives(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"method", "def configure\n  ruby '3.3.1'\nend\n"},
		{"class", "class Configure\n  ruby '3.3.1'\nend\n"},
		{"module", "module Configure\n  ruby '3.3.1'\nend\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(root, "Gemfile"), tc.body)
			observed := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
			snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
			if err != nil {
				t.Fatal(err)
			}
			collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-ruby-nested", observed, snapshot, false, "")
			if got := decodeDeclarationList(t, drainFactChannel(collected.Facts)); len(got) != 0 {
				t.Fatalf("nested Ruby directive became a repository runtime declaration: %+v", got)
			}
		})
	}
}
