// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"errors"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type stubCandidateLoader struct {
	page CandidatePage
	err  error
}

func (s stubCandidateLoader) LoadCandidates(context.Context, string) (CandidatePage, error) {
	return s.page, s.err
}

type stubFindingWriter struct {
	writes []DriftedWrite
	err    error
}

func (s *stubFindingWriter) WriteDriftedFindings(_ context.Context, w DriftedWrite) (DriftedWriteResult, error) {
	s.writes = append(s.writes, w)
	return DriftedWriteResult{Written: len(w.Pairs)}, s.err
}

func driftedIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "repo:repo-1",
		GenerationID: "gen-9",
		SourceSystem: "collector/git",
		Domain:       reducercontract.DomainCodeDrifted,
		Cause:        "fingerprints published",
		Payload:      map[string]any{"repo_id": "repo-1"},
	}
}

func handlerPair(idA, idB, pathB string, a, b []uint64) CandidatePair {
	mk := func(id, name, path string, shingles []uint64) MemberRow {
		return MemberRow{
			EntityID: id, EntityName: name, EntityType: "Function",
			RelativePath: path, Language: "go",
			StartLine: 10, EndLine: 40, TokenCount: 64,
			Shingles: shingles, FPExact: "exact-" + id, FPRenamed: "renamed-" + id,
		}
	}
	return CandidatePair{A: mk(idA, "big", "a.go", a), B: mk(idB, "bigCopy", pathB, b), SharedBands: 9}
}

// TestHandlePublishesAdmittedAndCountsSuppressed proves the handler contract:
// admitted pairs publish with similarity evidence; below-threshold and
// rule-suppressed pairs never publish but land in the suppression totals;
// loader pipeline stats merge into the same totals; budget exhaustions pass
// through for telemetry.
func TestHandlePublishesAdmittedAndCountsSuppressed(t *testing.T) {
	t.Parallel()

	admit := handlerPair("e1", "e2", "b.go", shingleSet(1, 9), append(shingleSet(1, 8), 101))
	low := handlerPair("e3", "e4", "c.go", shingleSet(1, 10), append(shingleSet(1, 6), 101, 102, 103, 104))
	gen := handlerPair("e5", "e6", "gen/d.pb.go", shingleSet(1, 9), append(shingleSet(1, 8), 101))
	loader := stubCandidateLoader{page: CandidatePage{
		Pairs: []CandidatePair{admit, low, gen},
		Stats: CandidateStats{
			BudgetExhausted:    []string{"e1"},
			BelowFloor:         2,
			NoShingles:         1,
			EqualityDuplicates: 3,
		},
	}}
	writer := &stubFindingWriter{}
	handler := CodeDriftedHandler{Loader: loader, Writer: writer}

	res, err := handler.Handle(context.Background(), driftedIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if res.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want succeeded", res.Status)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writer calls = %d, want 1", len(writer.writes))
	}
	got := writer.writes[0]
	if len(got.Pairs) != 1 {
		t.Fatalf("published pairs = %d, want 1 (only the admitted pair)", len(got.Pairs))
	}
	if got.Pairs[0].Similarity != 0.8 {
		t.Fatalf("published similarity = %v, want 0.8", got.Pairs[0].Similarity)
	}
	wantSuppressions := map[string]int{
		"similarity_below_threshold": 1,
		"generated_file":             1,
		"below_floor":                2,
		"no_shingles":                1,
		"equality_duplicate":         3,
	}
	for key, want := range wantSuppressions {
		if got.Suppressions[key] != want {
			t.Fatalf("suppressions[%q] = %d, want %d (full map %+v)", key, got.Suppressions[key], want, got.Suppressions)
		}
	}
	if len(got.BudgetExhausted) != 1 || got.BudgetExhausted[0] != "e1" {
		t.Fatalf("budget exhausted = %v, want [e1]", got.BudgetExhausted)
	}
	if got.RepoID != "repo-1" || got.GenerationID != "gen-9" {
		t.Fatalf("write scope = %q/%q, want repo-1/gen-9", got.RepoID, got.GenerationID)
	}
}

// TestHandleRejectsForeignDomain proves the handler refuses intents for
// other domains with an error (queue retry/dead-letter), never silent
// success.
func TestHandleRejectsForeignDomain(t *testing.T) {
	t.Parallel()

	handler := CodeDriftedHandler{}
	intent := driftedIntent()
	intent.Domain = reducercontract.DomainConfigStateDrift
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil for foreign domain, want error")
	}
}

// TestHandleSucceedsWithoutLoader proves the nil-loader tolerance: no
// observable input means success without drift, not a failure.
func TestHandleSucceedsWithoutLoader(t *testing.T) {
	t.Parallel()

	handler := CodeDriftedHandler{Writer: &stubFindingWriter{}}
	res, err := handler.Handle(context.Background(), driftedIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if res.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle() status = %q, want succeeded", res.Status)
	}
}

// TestHandleSurfacesLoaderError proves loader failures retry: a database
// error returns an error (never success with zero pairs, which would retire
// live findings on a transient failure).
func TestHandleSurfacesLoaderError(t *testing.T) {
	t.Parallel()

	handler := CodeDriftedHandler{Loader: stubCandidateLoader{err: errors.New("db down")}}
	if _, err := handler.Handle(context.Background(), driftedIntent()); err == nil {
		t.Fatal("Handle() error = nil on loader failure, want error")
	}
}
