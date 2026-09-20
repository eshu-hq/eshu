// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"fmt"
	"sync"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// recordingWriter is a goroutine-safe stubFindingWriter: concurrent Handle
// calls append under a mutex so the contention proof can count deliveries
// without tripping the race detector.
type recordingWriter struct {
	mu     sync.Mutex
	writes []DriftedWrite
}

func (w *recordingWriter) WriteDriftedFindings(_ context.Context, write DriftedWrite) (DriftedWriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, write)
	return DriftedWriteResult{Written: len(write.Pairs)}, nil
}

func (w *recordingWriter) snapshot() []DriftedWrite {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]DriftedWrite(nil), w.writes...)
}

// admittedBacklogPage builds a worst-case page: pairsPerEntity entities each
// nominated at the per-entity budget with Jaccard-0.8 shingle sets, so every
// pair admits and the write carries the full load.
func admittedBacklogPage(pairs int) CandidatePage {
	page := CandidatePage{}
	for i := 0; i < pairs; i++ {
		a := MemberRow{
			EntityID: fmt.Sprintf("e-%d-a", i), EntityName: "big", EntityType: "Function",
			RelativePath: "a.go", Language: "go",
			StartLine: 10, EndLine: 40, TokenCount: 64,
			Shingles: shingleSet(1, 9), FPExact: fmt.Sprintf("exact-%d-a", i), FPRenamed: fmt.Sprintf("renamed-%d-a", i),
		}
		b := MemberRow{
			EntityID: fmt.Sprintf("e-%d-b", i), EntityName: "bigCopy", EntityType: "Function",
			RelativePath: "b.go", Language: "go",
			StartLine: 50, EndLine: 80, TokenCount: 66,
			Shingles: append(shingleSet(1, 8), 101), FPExact: fmt.Sprintf("exact-%d-b", i), FPRenamed: fmt.Sprintf("renamed-%d-b", i),
		}
		page.Pairs = append(page.Pairs, CandidatePair{A: a, B: b, SharedBands: 9})
	}
	return page
}

// TestHandleConcurrentDuplicateDeliveriesConverge is the contention +
// idempotency leg: sixteen workers handle the same intent concurrently (the
// duplicate-delivery shape a queue redrive produces). Every delivery
// succeeds with byte-identical pair content, so the stable fact IDs the
// writer derives upsert-collapse instead of duplicating. The handler does
// not dedupe deliveries — the test pins that the idempotency lives in the
// stable IDs, not in handler-side locking — and -race proves the path is
// data-race free.
func TestHandleConcurrentDuplicateDeliveriesConverge(t *testing.T) {
	t.Parallel()

	loader := stubCandidateLoader{page: admittedBacklogPage(4)}
	writer := &recordingWriter{}
	handler := CodeDriftedHandler{Loader: loader, Writer: writer}

	const deliveries = 16
	var wg sync.WaitGroup
	errs := make([]error, deliveries)
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			res, err := handler.Handle(context.Background(), driftedIntent())
			if err != nil {
				errs[n] = err
				return
			}
			if res.Status != reducercontract.ResultStatusSucceeded {
				errs[n] = fmt.Errorf("delivery %d status = %q, want succeeded", n, res.Status)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	writes := writer.snapshot()
	if len(writes) != deliveries {
		t.Fatalf("writer calls = %d, want %d (handler passes every delivery through; stable IDs collapse them)", len(writes), deliveries)
	}
	first := DriftedFindingID(writes[0].RepoID, writes[0].Pairs[0].Pair.A, writes[0].Pairs[0].Pair.B)
	for i, write := range writes {
		if len(write.Pairs) != 4 {
			t.Fatalf("write %d pairs = %d, want 4", i, len(write.Pairs))
		}
		for _, pair := range write.Pairs {
			if got := DriftedFindingID(write.RepoID, pair.Pair.A, pair.Pair.B); got == "" {
				t.Fatalf("write %d has an empty pair identity", i)
			}
		}
		if got := DriftedFindingID(write.RepoID, write.Pairs[0].Pair.A, write.Pairs[0].Pair.B); got != first {
			t.Fatalf("write %d pair identity = %q, want %q (deliveries must converge)", i, got, first)
		}
	}
}

// TestHandlePartitionsWritesByRepo proves the repo_id partition: three
// intents for three repos each load and write only their own repo's pairs,
// so a backlog spanning repos never bleeds findings across the partition.
func TestHandlePartitionsWritesByRepo(t *testing.T) {
	t.Parallel()

	pages := map[string]CandidatePage{
		"repo-a": admittedBacklogPage(2),
		"repo-b": admittedBacklogPage(3),
		"repo-c": admittedBacklogPage(1),
	}
	loader := stubCandidateLoaderFunc(func(_ context.Context, repoID string) (CandidatePage, error) {
		return pages[repoID], nil
	})
	writer := &recordingWriter{}
	handler := CodeDriftedHandler{Loader: loader, Writer: writer}

	for repo := range pages {
		intent := driftedIntent()
		intent.IntentID = "intent-" + repo
		intent.ScopeID = "repo:" + repo
		intent.Payload = map[string]any{"repo_id": repo}
		res, err := handler.Handle(context.Background(), intent)
		if err != nil {
			t.Fatalf("Handle(%s) error = %v, want nil", repo, err)
		}
		if res.Status != reducercontract.ResultStatusSucceeded {
			t.Fatalf("Handle(%s) status = %q, want succeeded", repo, res.Status)
		}
	}
	writes := writer.snapshot()
	if len(writes) != len(pages) {
		t.Fatalf("writer calls = %d, want %d", len(writes), len(pages))
	}
	seen := map[string]int{}
	for _, write := range writes {
		seen[write.RepoID] += len(write.Pairs)
		if write.ScopeID != "repo:"+write.RepoID {
			t.Errorf("write scope = %q for repo %q, want the repo's own scope", write.ScopeID, write.RepoID)
		}
	}
	for repo, page := range pages {
		if seen[repo] != len(page.Pairs) {
			t.Errorf("repo %s pairs written = %d, want %d", repo, seen[repo], len(page.Pairs))
		}
	}
}

// TestWriterRetireNeverCrossesGenerations is the ordering leg: consecutive
// passes for two generations retire only their own (scope, generation), so
// a newer pass never deletes the older generation's rows out from under the
// active-generation read.
func TestWriterRetireNeverCrossesGenerations(t *testing.T) {
	t.Parallel()

	db := &testutil.FakeExecer{}
	writer := PostgresCodeDriftedWriter{DB: db}

	gen9 := driftedWrite()
	gen10 := driftedWrite()
	gen10.GenerationID = "gen-10"
	gen10.IntentID = "intent-2"

	if _, err := writer.WriteDriftedFindings(context.Background(), gen9); err != nil {
		t.Fatalf("gen-9 WriteDriftedFindings() error = %v, want nil", err)
	}
	if _, err := writer.WriteDriftedFindings(context.Background(), gen10); err != nil {
		t.Fatalf("gen-10 WriteDriftedFindings() error = %v, want nil", err)
	}
	if len(db.Execs) != 4 {
		t.Fatalf("ExecContext calls = %d, want 4 (insert + retire per generation)", len(db.Execs))
	}
	gen9Rows := testutil.DecodeBatchedVersionedFactCalls(t, db.Execs[:1])
	gen10Rows := testutil.DecodeBatchedVersionedFactCalls(t, db.Execs[2:3])
	assertDriftedRetireCall(t, db.Execs[1], gen9.ScopeID, "gen-9", []string{gen9Rows[0].FactID})
	assertDriftedRetireCall(t, db.Execs[3], gen10.ScopeID, "gen-10", []string{gen10Rows[0].FactID})
	if gen9Rows[0].FactID == gen10Rows[0].FactID {
		t.Fatal("generations must mint distinct fact IDs (stable key binds the generation)")
	}
}

// TestHandleFailuresRouteToRetryNotAck is the dead-letter leg: loader and
// writer failures return a zero Result with a non-nil error, so the generic
// queue machinery retries and eventually dead-letters instead of acking a
// silently unwritten generation.
func TestHandleFailuresRouteToRetryNotAck(t *testing.T) {
	t.Parallel()

	loaderFail := CodeDriftedHandler{Loader: stubCandidateLoader{err: fmt.Errorf("db down")}, Writer: &stubFindingWriter{}}
	res, err := loaderFail.Handle(context.Background(), driftedIntent())
	if err == nil {
		t.Fatal("loader failure must return an error")
	}
	if res.Status == reducercontract.ResultStatusSucceeded {
		t.Fatal("loader failure must not report success (that would ack and retire live findings)")
	}

	writerFail := CodeDriftedHandler{Loader: stubCandidateLoader{page: admittedBacklogPage(1)}, Writer: &stubFindingWriter{err: fmt.Errorf("write down")}}
	res, err = writerFail.Handle(context.Background(), driftedIntent())
	if err == nil {
		t.Fatal("writer failure must return an error")
	}
	if res.Status == reducercontract.ResultStatusSucceeded {
		t.Fatal("writer failure must not report success (that would ack an unwritten generation)")
	}
}

// BenchmarkCodeDriftedHandlerWorstCaseBacklog measures the drain time for a
// full-budget page: MaxCandidatesPerEntity (200) admitted pairs in one
// intent, the most verification and the largest single write one delivery
// can carry.
func BenchmarkCodeDriftedHandlerWorstCaseBacklog(b *testing.B) {
	loader := stubCandidateLoader{page: admittedBacklogPage(MaxCandidatesPerEntity)}
	handler := CodeDriftedHandler{Loader: loader, Writer: &stubFindingWriter{}}
	intent := driftedIntent()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := handler.Handle(context.Background(), intent)
		if err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
		if res.Status != reducercontract.ResultStatusSucceeded {
			b.Fatalf("Handle() status = %q, want succeeded", res.Status)
		}
	}
}

type stubCandidateLoaderFunc func(context.Context, string) (CandidatePage, error)

func (f stubCandidateLoaderFunc) LoadCandidates(ctx context.Context, repoID string) (CandidatePage, error) {
	return f(ctx, repoID)
}
