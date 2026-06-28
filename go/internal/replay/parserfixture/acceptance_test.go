// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture_test

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/parserfixture"
)

// repoFixturePath resolves a path under the repository's tests/fixtures tree from
// this test file's location, mirroring the parser package's helper.
func repoFixturePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller(0) failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	elements := append([]string{root, "tests", "fixtures"}, parts...)
	return filepath.Join(elements...)
}

// emitterCase pairs a demo language with its fixture tree.
type emitterCase struct {
	name     string
	scopeID  string
	repoID   string
	treePath string
}

func demoCases() []emitterCase {
	return []emitterCase{
		{
			name:     "go",
			scopeID:  "parser_fixture:go_comprehensive",
			repoID:   "go_comprehensive",
			treePath: repoFixturePath("ecosystems", "go_comprehensive"),
		},
		{
			name:     "hcl",
			scopeID:  "parser_fixture:terraform_comprehensive",
			repoID:   "terraform_comprehensive",
			treePath: repoFixturePath("ecosystems", "terraform_comprehensive"),
		},
	}
}

// drainEnvelopes collects every envelope a source yields across one full poll
// cycle, sorted by stable fact key for stable comparison.
func drainEnvelopes(t *testing.T, src collector.Source) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	ctx := context.Background()
	for {
		gen, ok, err := src.Next(ctx)
		if err != nil {
			t.Fatalf("source.Next: %v", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			out = append(out, env)
		}
		if gen.FactStreamErr != nil {
			if err := gen.FactStreamErr(); err != nil {
				t.Fatalf("fact stream error: %v", err)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StableFactKey < out[j].StableFactKey })
	return out
}

// TestRecordReplayRoundTripPreservesEnvelopesAndProvenance is the R-7 acceptance:
// for two real language parsers, recording from the live parser then replaying
// the fixture reproduces identical envelopes including SourceRef provenance.
func TestRecordReplayRoundTripPreservesEnvelopesAndProvenance(t *testing.T) {
	for _, tc := range demoCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Record side: run the real parser + the real envelope emission seam.
			emitter, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
				ScopeID:  tc.scopeID,
				RepoID:   tc.repoID,
				TreePath: tc.treePath,
			})
			if err != nil {
				t.Fatalf("NewEmitter: %v", err)
			}
			live := drainEnvelopes(t, emitter)
			if len(live) == 0 {
				t.Fatalf("emitter produced no envelopes for %s", tc.treePath)
			}

			// Persist as a canonical fixture, then replay credential-free.
			path := filepath.Join(t.TempDir(), tc.name+".json")
			if err := parserfixture.Record(context.Background(), parserfixture.RecordOptions{
				Emitter: emitter,
				Path:    path,
			}); err != nil {
				t.Fatalf("Record: %v", err)
			}
			src, err := parserfixture.NewSource(path)
			if err != nil {
				t.Fatalf("NewSource: %v", err)
			}
			replayed := drainEnvelopes(t, src)

			if len(replayed) != len(live) {
				t.Fatalf("envelope count drift: live=%d replayed=%d", len(live), len(replayed))
			}
			for i := range live {
				assertEnvelopeEqual(t, live[i], replayed[i])
			}
		})
	}
}

// assertEnvelopeEqual asserts two envelopes match on every durable field that the
// fixture is required to preserve, with provenance (SourceRef) checked field by
// field so a dropped/changed SourceURI or SourceRecordID fails loudly.
func assertEnvelopeEqual(t *testing.T, want, got facts.Envelope) {
	t.Helper()
	if want.FactKind != got.FactKind {
		t.Errorf("fact_kind: want %q got %q", want.FactKind, got.FactKind)
	}
	if want.StableFactKey != got.StableFactKey {
		t.Errorf("stable_fact_key: want %q got %q", want.StableFactKey, got.StableFactKey)
	}
	if want.FactID != got.FactID {
		t.Errorf("fact_id for %q: want %q got %q", want.StableFactKey, want.FactID, got.FactID)
	}
	if want.ScopeID != got.ScopeID {
		t.Errorf("scope_id: want %q got %q", want.ScopeID, got.ScopeID)
	}
	if want.GenerationID != got.GenerationID {
		t.Errorf("generation_id: want %q got %q", want.GenerationID, got.GenerationID)
	}
	if want.CollectorKind != got.CollectorKind {
		t.Errorf("collector_kind: want %q got %q", want.CollectorKind, got.CollectorKind)
	}
	// Provenance is first-class for R-7.
	if want.SourceRef.SourceSystem != got.SourceRef.SourceSystem {
		t.Errorf("provenance source_system for %q: want %q got %q", want.StableFactKey, want.SourceRef.SourceSystem, got.SourceRef.SourceSystem)
	}
	if want.SourceRef.SourceURI != got.SourceRef.SourceURI {
		t.Errorf("provenance source_uri for %q: want %q got %q", want.StableFactKey, want.SourceRef.SourceURI, got.SourceRef.SourceURI)
	}
	if want.SourceRef.SourceRecordID != got.SourceRef.SourceRecordID {
		t.Errorf("provenance source_record_id for %q: want %q got %q", want.StableFactKey, want.SourceRef.SourceRecordID, got.SourceRef.SourceRecordID)
	}
	if want.SourceRef.FactKey != got.SourceRef.FactKey {
		t.Errorf("provenance fact_key for %q: want %q got %q", want.StableFactKey, want.SourceRef.FactKey, got.SourceRef.FactKey)
	}
}
