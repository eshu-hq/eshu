// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

// Concurrency proof for the fault-injection tape's new shared state (R-11, #4120).
//
// The fault path adds exactly one piece of new shared mutable state to the
// RoundTripper: the per-key `attempts map[string]int`, mutated under `mu` in
// replay(). The existing TestConcurrentRecordIsRaceFree only covers ModeRecord,
// so this file adds the ModeReplay contention proof required by
// concurrency-deadlock-rigor: under N concurrent callers hitting the SAME
// faulted key, the attempt counter must increment exactly once per call, with
// no lost updates (which -race would surface) and no skipped or duplicated
// attempt indices (which the status-code accounting below surfaces).
//
// Skill active: golang-engineering, concurrency-deadlock-rigor.

import (
	"io"
	"net/http"
	"sync"
	"testing"
)

// TestConcurrentFaultReplayIsRaceFree drives N goroutines through a single
// faulted key whose FaultKindSequence assigns each attempt index a DISTINCT,
// observable HTTP status code (400+i). Because attempt index i maps one-to-one
// to status 400+i, the multiset of observed statuses is a direct readout of the
// attempt indices the increment handed out. The test asserts that readout is
// exactly {400, 401, ..., 400+N-1} — every index used once, none skipped, none
// duplicated — proving the mutex-guarded increment is exactly-once per call
// under contention. Run under -race to also catch a data race on the map.
func TestConcurrentFaultReplayIsRaceFree(t *testing.T) {
	t.Parallel()

	server, _ := newFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	recorder := New(Config{})
	recClient := &http.Client{Transport: recorder}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/contended", nil)
	if _, err := recClient.Do(req); err != nil {
		t.Fatalf("record: %v", err)
	}
	tape := recorder.Tape("test")
	server.Close()

	// Build a sequence where attempt index i yields status 400+i. With n=32 the
	// codes span 400..431, all within the valid [100,599] range validate accepts.
	const n = 32
	steps := make([]SequenceStep, n)
	for i := range steps {
		steps[i] = SequenceStep{Kind: FaultKindStatus, StatusCode: 400 + i}
	}
	tape.Interactions[0].Fault = &Fault{Kind: FaultKindSequence, Sequence: steps}

	replayer, err := NewReplayer(tape, Config{})
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}

	var (
		mu       sync.Mutex
		observed = make(map[int]int) // status code -> times seen
		wg       sync.WaitGroup
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rreq, _ := http.NewRequest(http.MethodGet, server.URL+"/contended", nil)
			resp, rtErr := replayer.RoundTrip(rreq)
			if rtErr != nil {
				t.Errorf("concurrent fault replay: %v", rtErr)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			mu.Lock()
			observed[resp.StatusCode]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Exactly-once accounting: every index 0..n-1 was handed out exactly once,
	// so every status 400..400+n-1 appears exactly once and nothing else does.
	if len(observed) != n {
		t.Fatalf("want %d distinct status codes, got %d: %v", n, len(observed), observed)
	}
	for i := 0; i < n; i++ {
		code := 400 + i
		switch observed[code] {
		case 1:
			// exactly-once: correct.
		case 0:
			t.Fatalf("attempt index %d (status %d) was never handed out (hole): %v", i, code, observed)
		default:
			t.Fatalf("attempt index %d (status %d) handed out %d times (duplicate): %v", i, code, observed[code], observed)
		}
	}
}
