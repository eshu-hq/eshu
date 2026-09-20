// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultOrphanReaperMinAge is how long a zombie child must be
	// continuously observed before the reaper touches it. Safety
	// precondition: every spawn in a reaper-hosting process waits
	// synchronously (Run/Output — verified tree-wide for the collector
	// processes that wire the reaper), so Go reaps tracked children within
	// milliseconds of exit and anything still a zombie after this age is
	// definitionally abandoned. A future Start-without-prompt-Wait spawn in
	// a reaper host would break this precondition; the failure mode is a
	// transient command error retried next cycle, never wrong truth.
	DefaultOrphanReaperMinAge = 2 * time.Minute

	// DefaultOrphanReaperInterval is how often the reaper scans for adopted
	// zombies. Thirty seconds bounds accumulation to a couple dozen pids even
	// under sustained spawn churn.
	DefaultOrphanReaperInterval = 30 * time.Second
)

// OrphanReaper reaps adopted zombie children that no os/exec Cmd tracks.
//
// Background: collector git operations fork helper grandchildren (the
// remote-https transport wrapper, shallow-boundary rev-list). A helper whose
// teardown outlives its parent's exit is reparented to PID 1. Go reaps
// per-pid and never touches unknown reparented children, and container images
// without an init process never reap them either, so each occurrence leaks
// one pids-cgroup slot until fork fails pod-wide. The reaper is the PID 1
// duty this binary otherwise lacks: it periodically adopts-then-reaps only
// zombies old enough that no legitimate waiter can still exist.
//
// A Reaper is safe for concurrent use.
type OrphanReaper struct {
	minAge   time.Duration
	interval time.Duration
	logger   *slog.Logger

	mu   sync.Mutex
	seen map[int]time.Time
}

// NewOrphanReaper builds a Reaper that reaps zombie children first observed
// at least minAge ago, scanning every interval. A nil logger disables log
// output; reaping still happens.
func NewOrphanReaper(minAge, interval time.Duration, logger *slog.Logger) *OrphanReaper {
	return &OrphanReaper{
		minAge:   minAge,
		interval: interval,
		logger:   logger,
		seen:     make(map[int]time.Time),
	}
}

// ScanOnce performs a single reap pass and returns the number of zombies
// reaped. Zombies seen for the first time are recorded, not reaped; zombies
// younger than minAge are left alone. It never returns an error: scan and
// reap failures are logged and retried on the next pass, and a zombie that
// vanishes between scan and reap is simply dropped from tracking.
func (r *OrphanReaper) ScanOnce() int {
	pids, err := zombieChildrenOfSelf()
	if err != nil {
		r.info("orphan reaper scan failed", slog.Any("error", err))
		return 0
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	alive := make(map[int]time.Time, len(pids))
	reaped := 0
	for _, pid := range pids {
		first, ok := r.seen[pid]
		if !ok {
			alive[pid] = now
			continue
		}
		if now.Sub(first) < r.minAge {
			alive[pid] = first
			continue
		}
		if reapZombie(pid) {
			reaped++
			r.info("reaped adopted zombie child",
				slog.Int("pid", pid),
				slog.Duration("zombie_age", now.Sub(first).Round(time.Second)))
		} else {
			// Keep the original first-seen time so the next pass retries
			// immediately instead of aging the zombie all over again.
			alive[pid] = first
		}
	}
	r.seen = alive
	return reaped
}

// Run scans on every interval tick until ctx is done. It returns when the
// context is cancelled.
func (r *OrphanReaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.ScanOnce()
		}
	}
}

// StartOrphanReaper registers this process as a child subreaper (best effort:
// without it, orphans still reach PID 1, which is this binary in containers)
// and starts a background Reaper with default cadence bound to ctx. A nil
// logger disables log output.
func StartOrphanReaper(ctx context.Context, logger *slog.Logger) {
	if err := EnableChildSubreaper(logger); err != nil {
		if logger != nil {
			logger.Warn("child subreaper unavailable, orphan reaping degraded",
				slog.Any("error", err))
		}
	}
	go NewOrphanReaper(DefaultOrphanReaperMinAge, DefaultOrphanReaperInterval, logger).Run(ctx)
}

func (r *OrphanReaper) info(msg string, attrs ...any) {
	if r.logger != nil {
		r.logger.Info(msg, attrs...)
	}
}

// reapZombie waits on pid, which must be a zombie child of this process. It
// reports whether the zombie is gone: success and already-reaped both mean
// gone. Any other outcome leaves the pid for a later pass to reconsider.
func reapZombie(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Wait returns immediately for a zombie. It only blocks if pid is still
	// running, which the pre-scan guarantees it is not; a concurrent state
	// change surfaces as an error and the pid is retried next pass.
	_, err = proc.Wait()
	return err == nil
}

// zombieChildrenOfSelf lists pids that are zombie children of this process,
// read from /proc. On platforms without /proc it returns an empty list and a
// nil error: absence of /proc means there is nothing to reap, not a failure,
// so ScanOnce stays a quiet no-op there. On Linux a read failure is a real
// error — silently disabling the reaper would re-admit the PID exhaustion it
// exists to prevent — so it is returned for ScanOnce to log.
func zombieChildrenOfSelf() ([]int, error) {
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		if runtime.GOOS == "linux" {
			return nil, err
		}
		return nil, nil
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		state, ppid, ok := procStateAndParent(pid)
		if !ok {
			continue
		}
		if state == "Z" && ppid == self {
			out = append(out, pid)
		}
	}
	return out, nil
}

// procStateAndParent reads /proc/pid/stat and reports the process state and
// parent pid. It reports ok=false when the process vanished mid-read or the
// stat line does not parse. The comm field may itself contain spaces or
// parentheses, so parsing anchors on the last closing paren.
func procStateAndParent(pid int) (state string, ppid int, ok bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", 0, false
	}
	paren := strings.LastIndex(string(data), ")")
	if paren < 0 {
		return "", 0, false
	}
	fields := strings.Fields(string(data)[paren+2:])
	if len(fields) < 2 {
		return "", 0, false
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, false
	}
	return fields[0], ppid, true
}
