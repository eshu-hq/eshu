// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// requireLinuxSubreaper skips tests that need Linux /proc reparenting and
// prctl subreaper semantics. orphaned grandchildren reparent to init on other
// platforms, so the test process can never observe them as its own zombies.
func requireLinuxSubreaper(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("orphan zombie reaping needs Linux /proc and subreaper semantics")
	}
}

// spawnAbandonedGrandchild starts a shell that backgrounds a short sleep and
// exits immediately. The sleep grandchild reparents to the test process (once
// it is a subreaper) and, because no os/exec Cmd tracks it, remains an
// unreaped zombie after it exits. It polls until the zombie is observable and
// returns its pid.
func spawnAbandonedGrandchild(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 0.5 &")
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup shell Run() error = %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pids, err := zombieChildrenOfSelf()
		if err != nil {
			t.Fatalf("zombieChildrenOfSelf() error = %v", err)
		}
		if len(pids) > 0 {
			return pids[len(pids)-1]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for abandoned grandchild to become a zombie")
	return -1
}

// drainOrphans reaps every zombie child older than minAge so one test never
// leaks zombies into the next. It fails the test if anything remains.
func drainOrphans(t *testing.T, minAge time.Duration) {
	t.Helper()
	r := NewOrphanReaper(minAge, time.Second, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r.ScanOnce()
		rest, err := zombieChildrenOfSelf()
		if err != nil {
			t.Fatalf("zombieChildrenOfSelf() error = %v", err)
		}
		if len(rest) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out draining orphan zombies")
}

// TestOrphanReaperReapsAbandonedZombie is the regression for the ops-qa pids
// exhaustion: every git fetch orphaned one git grandchild that no os/exec Cmd
// tracked, and PID 1 never reaped it. The reaper must reap exactly the zombie
// the test abandons.
func TestOrphanReaperReapsAbandonedZombie(t *testing.T) {
	requireLinuxSubreaper(t)
	if err := EnableChildSubreaper(nil); err != nil {
		t.Skipf("PR_SET_CHILD_SUBREAPER unavailable: %v", err)
	}
	want := spawnAbandonedGrandchild(t)

	r := NewOrphanReaper(0, time.Second, nil)
	reaped := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && reaped == 0 {
		reaped += r.ScanOnce()
		if reaped == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if reaped != 1 {
		t.Fatalf("ScanOnce() reaped %d zombies, want exactly 1 (pid %d)", reaped, want)
	}
	if rest, err := zombieChildrenOfSelf(); err != nil {
		t.Fatalf("zombieChildrenOfSelf() error = %v", err)
	} else if len(rest) != 0 {
		t.Fatalf("zombieChildrenOfSelf() = %v after reap, want none", rest)
	}
}

// TestOrphanReaperHonorsMinAge proves the safety gate: a young zombie must
// survive a scan, so the reaper can never steal the exit status of a
// legitimately tracked child whose waiter has not run yet.
func TestOrphanReaperHonorsMinAge(t *testing.T) {
	requireLinuxSubreaper(t)
	if err := EnableChildSubreaper(nil); err != nil {
		t.Skipf("PR_SET_CHILD_SUBREAPER unavailable: %v", err)
	}
	spawnAbandonedGrandchild(t)

	r := NewOrphanReaper(time.Hour, time.Second, nil)
	if got := r.ScanOnce(); got != 0 {
		t.Fatalf("ScanOnce() with minAge=1h reaped %d, want 0", got)
	}
	rest, err := zombieChildrenOfSelf()
	if err != nil {
		t.Fatalf("zombieChildrenOfSelf() error = %v", err)
	}
	if len(rest) == 0 {
		t.Fatal("young zombie vanished without a reap; test setup is broken")
	}
	drainOrphans(t, 0)
}

// TestOrphanReaperIgnoresTrackedChildren guards the race the reaper must never
// lose: a normally waited child (reaped by os/exec itself) must never be
// double-reaped or counted.
func TestOrphanReaperIgnoresTrackedChildren(t *testing.T) {
	requireLinuxSubreaper(t)
	if err := exec.Command("true").Run(); err != nil {
		t.Fatalf("setup true Run() error = %v", err)
	}
	r := NewOrphanReaper(0, time.Second, nil)
	if got := r.ScanOnce(); got != 0 {
		t.Fatalf("ScanOnce() reaped %d tracked children, want 0", got)
	}
}
