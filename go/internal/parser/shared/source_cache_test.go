// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"fmt"
	"sync"
	"testing"
)

// TestPrimeSourceFirstWriterWinsUnderRefcount is the regression seed for the
// #4657 same-path torn-read defect: the pre-fix sourceCache was a plain
// sync.Map, so two concurrent Engine.ParsePath calls on the same absolute
// path could each PrimeSource with their own bytes, with the later Store
// silently overwriting the earlier goroutine's snapshot mid-parse. That let
// one ParsePath call read the language-parser bytes (version A, primed
// first) but the content-metadata bytes (version B, overwritten later) for
// the same nominal file, mixing two versions in one payload.
//
// The fix requires first-writer-wins semantics under a refcount: once an
// entry exists for a path, a second PrimeSource call must increment the
// refcount but must NOT replace the already-cached body, so every concurrent
// same-path caller observes one consistent snapshot for the lifetime of the
// first primer's entry.
func TestPrimeSourceFirstWriterWinsUnderRefcount(t *testing.T) {
	path := t.TempDir() + "/first-writer-wins.go"

	first := []byte("package service // version A")
	second := []byte("package service // version B")

	PrimeSource(path, first)
	PrimeSource(path, second)

	got, err := ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource(%q) error = %v, want nil", path, err)
	}
	if string(got) != string(first) {
		t.Fatalf("ReadSource(%q) = %q, want first-primed body %q (first-writer-wins)", path, got, first)
	}

	ClearSource(path)
	ClearSource(path)
}

// TestClearSourceSurvivesUntilLastRefcountDecrement covers the refcount half
// of the #4657 fix: the pre-fix sourceCache deleted a path's entry on the
// very first ClearSource call, so one goroutine finishing its ParsePath call
// could delete the cache entry a sibling goroutine (still mid-parse, on the
// same path) was relying on for its own content-metadata read. The entry
// must survive every ClearSource call except the last matching one.
func TestClearSourceSurvivesUntilLastRefcountDecrement(t *testing.T) {
	path := t.TempDir() + "/refcount-survives.go"
	body := []byte("package service // refcount")

	PrimeSource(path, body) // refs=1 (goroutine A primes)
	PrimeSource(path, body) // refs=2 (goroutine B primes, same path)

	ClearSource(path) // refs=1 (goroutine A finishes) -- entry MUST survive

	got, err := ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource(%q) error = %v, want nil (entry must survive one ClearSource while refs>0)", path, err)
	}
	if string(got) != string(body) {
		t.Fatalf("ReadSource(%q) = %q, want %q", path, got, body)
	}

	ClearSource(path) // refs=0 (goroutine B finishes) -- entry MUST now be gone

	if _, ok := sourceCacheEntryForTest(path); ok {
		t.Fatalf("sourceCache entry for %q survived the last ClearSource, want deleted", path)
	}
}

// TestClearSourceOnMissingEntryIsNoOp documents that ClearSource must remain
// safe to call when no entry was ever primed for path (unchanged contract).
func TestClearSourceOnMissingEntryIsNoOp(t *testing.T) {
	path := t.TempDir() + "/never-primed.go"

	ClearSource(path) // must not panic or corrupt other entries
}

// TestConcurrentPrimeAndClearSourceOnOnePathIsRaceFree exercises many
// goroutines concurrently priming and clearing the same absolute path,
// mirroring concurrent Engine.ParsePath calls on one file. Run with -race.
// It asserts every ReadSource observed during the overlap returns some
// primed body for the path (never a read error caused by a premature
// delete), which is the property the mutex+refcount design guarantees and
// the plain sync.Map design did not.
func TestConcurrentPrimeAndClearSourceOnOnePathIsRaceFree(t *testing.T) {
	path := t.TempDir() + "/concurrent-same-path.go"
	body := []byte("package service // concurrent")

	const goroutines = 16
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			PrimeSource(path, body)
			defer ClearSource(path)

			got, err := ReadSource(path)
			if err != nil {
				errs[index] = err
				return
			}
			if string(got) != string(body) {
				errs[index] = fmt.Errorf("ReadSource(%q) = %q, want %q", path, got, body)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
}
