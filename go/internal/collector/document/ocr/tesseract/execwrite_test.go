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
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
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
	var busy []string
	for w := 0; w < 3; w++ {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			for n := 0; time.Now().Before(deadline); n++ {
				path := filepath.Join(dir, fmt.Sprintf("bin-%d-%d", w, n))
				writeExecutable(t, path, body)
				if err := exec.Command(path, "--version").Run(); err != nil && strings.Contains(err.Error(), "text file busy") {
					mu.Lock()
					busy = append(busy, err.Error())
					mu.Unlock()
				}
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
}
