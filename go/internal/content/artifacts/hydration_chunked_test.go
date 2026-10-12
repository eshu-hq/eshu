// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tfFiles builds n Terraform source files. The HCL extractor reads each one,
// so each is a consumed file and each is fetched with content.
func tfFiles(n int) []File {
	files := make([]File, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, fileRow(
			fmt.Sprintf("infra/m%03d/main.tf", i),
			fmt.Sprintf("module \"m%d\" {\n  source = \"./mod%d\"\n}\n", i, i),
		))
	}
	return files
}

// TestBatchedHydrationIssuesOneStatementPerChunkOfTen is the statement-shape
// contract of #7126 PR 2: the consumed files of one repository are read with
// one ReadFiles statement per chunk of ten, and GetFileContent is not
// used. runConfigLoad runs at the floor allowance of 50, which bounds a
// repository at five chunks.
func TestBatchedHydrationIssuesOneStatementPerChunkOfTen(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		files, wantChunks int
	}{
		{0, 0}, {1, 1}, {9, 1}, {10, 1}, {11, 2}, {20, 2}, {21, 3}, {49, 5}, {50, 5}, {51, 5}, {120, 5},
	} {
		tc := tc
		t.Run(fmt.Sprintf("%d files", tc.files), func(t *testing.T) {
			t.Parallel()
			store := newHydrationStore(tfFiles(tc.files)...)
			got := runConfigLoad(t, store, store.listed(t), productionLoadPasses)
			if got.err != nil {
				t.Fatalf("load error = %v", got.err)
			}
			// runConfigLoad walks two sources over the same store: the anchor
			// with a file list and one related repository that lists it.
			if want := 2 * tc.wantChunks; len(store.batchCalls) != want {
				t.Fatalf("ReadFiles statements = %d, want %d (5 chunks per repository at most)", len(store.batchCalls), want)
			}
			if store.singleCalls != 0 {
				t.Fatalf("GetFileContent statements = %d, want none: the batch replaces the per-file read", store.singleCalls)
			}
			read := 0
			for _, call := range store.batchCalls {
				if len(call) > 10 {
					t.Fatalf("chunk of %d paths, want at most 10", len(call))
				}
				read += len(call)
			}
			wantRead := tc.files
			if wantRead > HydrationFloor {
				wantRead = HydrationFloor
			}
			if read != 2*wantRead {
				t.Fatalf("paths read = %d, want %d (the first 50 candidates, twice)", read, 2*wantRead)
			}
			if got.stats.BatchStatements != 2*tc.wantChunks {
				t.Fatalf("stats.BatchStatements = %d, want %d", got.stats.BatchStatements, 2*tc.wantChunks)
			}
			if tc.wantChunks > 0 && got.stats.BatchChunkSize != 10 {
				t.Fatalf("stats.BatchChunkSize = %d, want 10", got.stats.BatchChunkSize)
			}
		})
	}
}

// TestBatchedHydrationPhaseTwoReadsAreChunked covers the second read of the
// pass: resource files a kustomization names are fetched in chunks too.
func TestBatchedHydrationPhaseTwoReadsAreChunked(t *testing.T) {
	t.Parallel()

	refs := make([]string, 0, 23)
	files := []File{}
	for i := 0; i < 23; i++ {
		name := fmt.Sprintf("res-%02d.yaml", i)
		refs = append(refs, name)
		files = append(files, fileRow("deploy/"+name, policyResource(fmt.Sprintf("r%d", i))))
	}
	files = append(files, fileRow("deploy/kustomization.yaml", kustomizationFor(refs...)))
	store := newHydrationStore(files...)
	anchor := store.listed(t)
	got := runConfigLoad(t, store, anchor, productionLoadPasses)
	if got.err != nil {
		t.Fatalf("load error = %v", got.err)
	}
	// Per repository: one chunk for the kustomization, three for 23 resources.
	if want := 2 * 4; len(store.batchCalls) != want {
		t.Fatalf("ReadFiles statements = %d, want %d: %v", len(store.batchCalls), want, store.batchCalls)
	}
	if store.singleCalls != 0 {
		t.Fatalf("GetFileContent statements = %d, want none", store.singleCalls)
	}
}

// gatedBatchStore implements only ReadFiles. A read blocks until
// release closes or its context ends, so a test sees how many reads one
// pass keeps in flight.
type gatedBatchStore struct {
	Reader

	release chan struct{}
	started chan string
	// failNow gates a failing read: it fails once failNow is closed. The
	// constructor hands out a closed channel, so a failing read fails at once
	// unless a test swaps in an open one.
	failNow chan struct{}

	mu       sync.Mutex
	calls    int
	inFlight int
	peak     int
	failAt   map[string]error
	delay    map[string]time.Duration
}

func newGatedBatchStore() *gatedBatchStore {
	failNow := make(chan struct{})
	close(failNow)
	return &gatedBatchStore{
		release: make(chan struct{}),
		started: make(chan string, 4096),
		failNow: failNow,
		failAt:  map[string]error{},
		delay:   map[string]time.Duration{},
	}
}

func (s *gatedBatchStore) ReadFiles(ctx context.Context, repoID string, paths []string) ([]File, error) {
	first := paths[0]
	s.mu.Lock()
	s.calls++
	s.inFlight++
	if s.inFlight > s.peak {
		s.peak = s.inFlight
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()
	s.started <- first
	if err := s.failAt[first]; err != nil {
		<-s.failNow
		time.Sleep(s.delay[first])
		return nil, err
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	rows := make([]File, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, File{RepoID: repoID, RelativePath: path, Content: "k = 1\n"})
	}
	return rows, nil
}

// awaitStarted waits for n reads to have started and fails the test, instead
// of hanging, when fewer do.
func (s *gatedBatchStore) awaitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-s.started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d reads started", i, n)
		}
	}
}

func gatedFixture(n int) ([]File, []int) {
	files := make([]File, 0, n)
	indexes := make([]int, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, fileRow(fmt.Sprintf("p%03d.tf", i), ""))
		indexes = append(indexes, i)
	}
	return files, indexes
}

// TestChunkedReadKeepsAtMostEightChunksInFlight proves the fan-out bound on a
// call the cap would never produce (200 paths, 20 chunks): the pass reaches
// eight reads at once and never nine.
func TestChunkedReadKeepsAtMostEightChunksInFlight(t *testing.T) {
	t.Parallel()

	store := newGatedBatchStore()
	files, indexes := gatedFixture(200)
	var stats ReadStats
	done := make(chan error, 1)
	go func() {
		done <- productionChunkedRead.fetch(context.Background(), store, "repo", files, indexes, &stats)
	}()
	store.awaitStarted(t, 8)
	// A ninth read must not start while the eight block.
	select {
	case path := <-store.started:
		t.Fatalf("a ninth read started (%s) while eight were blocked", path)
	case <-time.After(150 * time.Millisecond):
	}
	close(store.release)
	if err := <-done; err != nil {
		t.Fatalf("fetch error = %v", err)
	}
	if store.peak != 8 {
		t.Fatalf("peak concurrent reads = %d, want 8", store.peak)
	}
	if store.calls != 20 || stats.BatchStatements != 20 {
		t.Fatalf("statements = %d (stats %d), want 20 chunks", store.calls, stats.BatchStatements)
	}
	if stats.BatchChunksInFlightMax != 8 {
		t.Fatalf("stats.BatchChunksInFlightMax = %d, want 8", stats.BatchChunksInFlightMax)
	}
	for i, file := range files {
		if file.Content != "k = 1\n" {
			t.Fatalf("file %d (%s) was not hydrated", i, file.RelativePath)
		}
	}
}

// TestChunkedReadReturnsTheLowestFailingChunkDeterministically fixes which
// error wins when several chunks fail: the one of the lowest-numbered failing
// chunk, however the reads interleave. The higher chunk fails first here.
func TestChunkedReadReturnsTheLowestFailingChunkDeterministically(t *testing.T) {
	t.Parallel()

	errLow := errors.New("chunk 1 failed")
	errHigh := errors.New("chunk 3 failed")
	for run := 0; run < 25; run++ {
		store := newGatedBatchStore()
		store.failAt["p010.tf"] = errLow
		store.delay["p010.tf"] = 20 * time.Millisecond
		store.failAt["p030.tf"] = errHigh
		close(store.release)
		files, indexes := gatedFixture(50)
		before := append([]File(nil), files...)
		var stats ReadStats
		err := productionChunkedRead.fetch(context.Background(), store, "repo", files, indexes, &stats)
		if !errors.Is(err, errLow) || errors.Is(err, errHigh) {
			t.Fatalf("run %d: error = %v, want the lowest failing chunk's", run, err)
		}
		for i := range files {
			if files[i] != before[i] {
				t.Fatalf("run %d: file %d changed although the pass failed; a failed pass must leave the rows alone", run, i)
			}
		}
	}
}

// TestChunkedReadStopsDispatchingAfterAFailure shows a recorded failure ends
// the launches. One chunk is in flight at a time and the first fails. The
// dispatcher is waiting for the slot of that read, a read records its failure
// before it frees its slot, and the launch decision reads the failure under the
// same mutex, so the freed slot is never used: exactly one statement runs of
// 20 chunks. A dispatcher that handed the next chunk to the failing worker,
// or checked the failure before taking the slot, would issue two.
func TestChunkedReadStopsDispatchingAfterAFailure(t *testing.T) {
	t.Parallel()

	store := newGatedBatchStore()
	boom := errors.New("boom")
	store.failAt["p000.tf"] = boom
	// Every read that does run is answered, so a regression fails the count
	// below instead of waiting on a blocked read.
	close(store.release)
	files, indexes := gatedFixture(200)
	var stats ReadStats
	err := chunkedRead{chunkSize: 10, inFlight: 1}.fetch(context.Background(), store, "repo", files, indexes, &stats)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the injected failure", err)
	}
	if store.calls != 1 {
		t.Fatalf("%d chunks issued, want exactly the one that failed: none after it", store.calls)
	}
	if stats.BatchStatements != 1 {
		t.Fatalf("stats.BatchStatements = %d, want 1", stats.BatchStatements)
	}
}

// TestChunkedReadHonorsContextCancellation covers both cancellation points: a
// context that is already done issues no statement, and one cancelled while
// reads are in flight returns the context error, launches nothing further and
// leaves no goroutine behind. Both counts are exact: the launch decision reads
// the context under the dispatcher's lock, with no ready-worker race.
func TestChunkedReadHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("already cancelled", func(t *testing.T) {
		t.Parallel()
		// The store ignores the context and would answer every read, so a
		// statement issued on the cancelled context would come back as
		// success.
		store := newGatedBatchStore()
		close(store.release)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		files, indexes := gatedFixture(30)
		var stats ReadStats
		err := productionChunkedRead.fetch(ctx, store, "repo", files, indexes, &stats)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if store.calls != 0 {
			t.Fatalf("%d statements issued on a cancelled context, want none", store.calls)
		}
		if stats.BatchStatements != 0 || stats.BatchChunkSize != 0 || stats.BatchChunksInFlightMax != 0 {
			t.Fatalf("stats = %+v, want no batch fields when no statement was issued", stats)
		}
	})
	t.Run("cancelled in flight", func(t *testing.T) {
		t.Parallel()
		store := newGatedBatchStore()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		files, indexes := gatedFixture(200)
		var stats ReadStats
		done := make(chan error, 1)
		go func() {
			done <- productionChunkedRead.fetch(ctx, store, "repo", files, indexes, &stats)
		}()
		store.awaitStarted(t, 8)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the pass did not return after the context was cancelled")
		}
		if store.calls != 8 {
			t.Fatalf("%d statements issued, want exactly the eight in flight", store.calls)
		}
		store.mu.Lock()
		inFlight := store.inFlight
		store.mu.Unlock()
		if inFlight != 0 {
			t.Fatalf("%d reads still in flight after the pass returned", inFlight)
		}
	})
}

// TestChunkedReadAssignsRowsByPathNotByPosition feeds a store that answers in
// reverse and with an unrequested extra row: every list row still receives its
// own stored row, and a row nobody asked for is ignored.
func TestChunkedReadAssignsRowsByPathNotByPosition(t *testing.T) {
	t.Parallel()

	store := &reversingBatchStore{}
	files := []File{
		fileRow("a.tf", ""), fileRow("b.tf", ""), fileRow("a.tf", ""), fileRow("c.tf", ""),
	}
	indexes := []int{0, 1, 2, 3}
	var stats ReadStats
	if err := productionChunkedRead.fetch(context.Background(), store, "repo", files, indexes, &stats); err != nil {
		t.Fatalf("fetch error = %v", err)
	}
	want := []string{"content-of-a.tf", "content-of-b.tf", "content-of-a.tf", "content-of-c.tf"}
	for i, file := range files {
		if file.Content != want[i] {
			t.Fatalf("files[%d].Content = %q, want %q", i, file.Content, want[i])
		}
	}
	if len(store.requested) != 1 || strings.Join(store.requested[0], ",") != "a.tf,b.tf,c.tf" {
		t.Fatalf("requested = %v, want one chunk of the distinct paths in list order", store.requested)
	}
}

type reversingBatchStore struct {
	Reader
	mu        sync.Mutex
	requested [][]string
	calls     atomic.Int64
}

func (s *reversingBatchStore) ReadFiles(_ context.Context, repoID string, paths []string) ([]File, error) {
	s.mu.Lock()
	s.requested = append(s.requested, append([]string(nil), paths...))
	s.mu.Unlock()
	s.calls.Add(1)
	rows := make([]File, 0, len(paths)+1)
	sorted := append([]string(nil), paths...)
	sort.Sort(sort.Reverse(sort.StringSlice(sorted)))
	for _, path := range sorted {
		rows = append(rows, File{RepoID: repoID, RelativePath: path, Content: "content-of-" + path})
	}
	rows = append(rows, File{RepoID: repoID, RelativePath: "unrequested.tf", Content: "stray"})
	return rows, nil
}

// cancelAfterFirstReadStore cancels the context after its first read finishes
// successfully. Every read it answers succeeds, so a pass that stopped on the
// context has no chunk error to report and must still not claim success.
type cancelAfterFirstReadStore struct {
	Reader
	cancel context.CancelFunc
	once   sync.Once
	calls  atomic.Int64
}

func (s *cancelAfterFirstReadStore) ReadFiles(_ context.Context, repoID string, paths []string) ([]File, error) {
	s.calls.Add(1)
	rows := make([]File, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, File{RepoID: repoID, RelativePath: path, Content: "k = 1\n"})
	}
	s.once.Do(s.cancel)
	return rows, nil
}

// TestChunkedReadCancelledBetweenChunksIsNotSuccess covers the stop that no
// read error reports: the context ends while every read that ran succeeded,
// and chunks remain undispatched. The pass returns the context error and
// leaves the rows alone, so a partial hydration is never read as complete.
func TestChunkedReadCancelledBetweenChunksIsNotSuccess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cancelAfterFirstReadStore{cancel: cancel}
	files, indexes := gatedFixture(200)
	before := append([]File(nil), files...)
	var stats ReadStats
	err := chunkedRead{chunkSize: 10, inFlight: 1}.fetch(ctx, store, "repo", files, indexes, &stats)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := store.calls.Load(); got != 1 {
		t.Fatalf("%d chunks issued, want exactly the one that ended the context", got)
	}
	for i := range files {
		if files[i] != before[i] {
			t.Fatalf("file %d changed although the pass did not complete", i)
		}
	}
}

// readShapeViolation reports why a load did not read through ReadFiles
// chunks, or "" when it did: no GetFileContent statement, and every chunk of
// at most ten paths.
func readShapeViolation(store *hydrationStore) string {
	if store.singleCalls != 0 {
		return fmt.Sprintf("%d GetFileContent statements; the config-artifact read must use ReadFiles chunks", store.singleCalls)
	}
	for _, call := range store.batchCalls {
		if len(call) > 10 {
			return fmt.Sprintf("a ReadFiles chunk of %d paths, want at most 10", len(call))
		}
	}
	if len(store.batchCalls) == 0 {
		return "no ReadFiles statement was issued"
	}
	return ""
}

// TestChunkedReadShapeGuardSeededPair is the seeded-violation RED/GREEN pair
// for the statement-shape guard. GREEN: the production loader passes it. RED:
// the same load through the per-file reader of the earlier change, which
// issues one GetFileContent statement per file, is reported.
func TestChunkedReadShapeGuardSeededPair(t *testing.T) {
	t.Parallel()

	files := tfFiles(25)
	green := newHydrationStore(files...)
	if got := runConfigLoad(t, green, green.listed(t), productionLoadPasses); got.err != nil {
		t.Fatalf("green load error = %v", got.err)
	}
	if violation := readShapeViolation(green); violation != "" {
		t.Fatalf("green leg: guard flagged the production loader: %s", violation)
	}

	red := newHydrationStore(files...)
	if got := runConfigLoad(t, red, red.listed(t), perFilePasses); got.err != nil {
		t.Fatalf("red load error = %v", got.err)
	}
	if violation := readShapeViolation(red); !strings.Contains(violation, "GetFileContent statements") {
		t.Fatalf("red leg: guard did not flag the per-file reader (violation = %q)", violation)
	}
}
