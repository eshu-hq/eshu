// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// writeExecutable writes body to path as an executable the caller execs next.
// It holds syscall.ForkLock for reading across the open, write, and close, so
// no goroutine can fork while the write descriptor is open and no child can
// inherit it. Go forks hold the lock for writing until the child has exec'd.
func writeExecutable(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := writeExecutableFile(path, body); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

// writeExecutableFile is writeExecutable without the testing.T, for goroutines
// that must not call t.Fatalf. It returns the write error to the caller.
func writeExecutableFile(path string, body []byte) error {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	return os.WriteFile(path, body, 0o700)
}

// TestWriteExecutableSurvivesConcurrentForks proves that exec of a file just
// written by writeExecutable never fails with ETXTBSY while other goroutines
// fork. A fork between the write open and its close hands the child a copy of
// the write descriptor until the child execs, so a racing exec of the written
// file reports "text file busy" (golang/go#22315).
func TestWriteExecutableSurvivesConcurrentForks(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	body, err := os.ReadFile(fx.binary)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	dir := t.TempDir()
	deadline := time.Now().Add(1500 * time.Millisecond)
	stop := make(chan struct{})

	var forkers sync.WaitGroup
	for i := 0; i < 6; i++ {
		forkers.Add(1)
		go func() {
			defer forkers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = exec.Command(fx.binary, "--version").Run()
			}
		}()
	}

	var writers sync.WaitGroup
	var mu sync.Mutex
	var busy, failed []string
	var succeeded int
	for w := 0; w < 3; w++ {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			for n := 0; time.Now().Before(deadline); n++ {
				path := filepath.Join(dir, fmt.Sprintf("bin-%d-%d", w, n))
				werr := writeExecutableFile(path, body)
				var rerr error
				if werr == nil {
					rerr = exec.Command(path, "--version").Run()
				}
				mu.Lock()
				switch {
				case werr != nil:
					failed = append(failed, werr.Error())
				case rerr == nil:
					succeeded++
				case strings.Contains(rerr.Error(), "text file busy"):
					busy = append(busy, rerr.Error())
				default:
					failed = append(failed, rerr.Error())
				}
				mu.Unlock()
				_ = os.Remove(path)
			}
		}(w)
	}
	writers.Wait()
	close(stop)
	forkers.Wait()
	if len(busy) > 0 {
		t.Fatalf("%d exec(s) hit ETXTBSY after writeExecutable, first: %s", len(busy), busy[0])
	}
	// A run where no exec succeeded, or any other write or exec failed, proves
	// nothing about ETXTBSY, so it must not pass.
	if len(failed) > 0 {
		t.Fatalf("%d write or exec(s) failed for a reason other than ETXTBSY, first: %s", len(failed), failed[0])
	}
	if succeeded == 0 {
		t.Fatal("no exec succeeded, so the stress run proved nothing")
	}
}
