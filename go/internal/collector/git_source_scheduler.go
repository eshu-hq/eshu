// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// snapshotScheduler holds the shared state for the two-lane snapshot worker
// loops. It exists so the small-preferring and large-preferring worker bodies
// share one semaphore-release audit surface: every `largeSem <- struct{}{}` in
// this file has exactly one matching `<-largeSem` on every code path.
type snapshotScheduler struct {
	source      *GitSource
	smallCh     chan SelectedRepository
	largeCh     chan SelectedRepository
	largeSem    chan struct{}
	workerCtx   context.Context
	cancel      context.CancelFunc
	sourceRunID string
	observedAt  time.Time
	errOnce     *sync.Once
	firstErr    *error
	completed   *atomic.Int64
}

// processSmall snapshots one small repo with no semaphore involvement.
func (sc *snapshotScheduler) processSmall(repo SelectedRepository, workerID int) {
	sc.source.processRepo(sc.workerCtx, repo, nil,
		sc.sourceRunID, sc.observedAt, workerID,
		sc.errOnce, sc.firstErr, sc.cancel, sc.completed)
}

// acquireForLarge acquires the large-repo semaphore for one giant parse,
// recording the wait time. It returns false only when the worker context is
// cancelled while waiting, in which case the caller MUST NOT release the
// semaphore (it was never acquired).
func (sc *snapshotScheduler) acquireForLarge(repo SelectedRepository, workerID int) bool {
	semWaitStart := time.Now()
	select {
	case sc.largeSem <- struct{}{}:
	case <-sc.workerCtx.Done():
		return false
	}
	waitSeconds := time.Since(semWaitStart).Seconds()
	if sc.source.Instruments != nil {
		sc.source.Instruments.LargeRepoSemaphoreWait.Record(sc.workerCtx, waitSeconds)
	}
	if sc.source.Logger != nil {
		sc.source.Logger.InfoContext(sc.workerCtx, "large repo semaphore acquired",
			slog.String("repo_path", repo.RepoPath),
			slog.Int("worker_id", workerID),
			slog.Float64("wait_seconds", waitSeconds),
		)
	}
	return true
}

// processLargeHeld snapshots one giant repo while the caller already holds the
// large-repo semaphore. The afterSnapshot callback releases the semaphore
// before the (potentially slow) stream send so another worker can start the
// next giant while this worker waits for buffer space.
func (sc *snapshotScheduler) processLargeHeld(repo SelectedRepository, workerID int, semAcquiredAt time.Time) {
	sc.source.processRepo(sc.workerCtx, repo,
		func() {
			<-sc.largeSem
			if sc.source.Logger != nil {
				sc.source.Logger.InfoContext(sc.workerCtx, "large repo semaphore released",
					slog.Int("worker_id", workerID),
					slog.Float64("held_seconds", time.Since(semAcquiredAt).Seconds()),
				)
			}
		},
		sc.sourceRunID, sc.observedAt, workerID,
		sc.errOnce, sc.firstErr, sc.cancel, sc.completed)
}

// drainSmall consumes the small lane to completion. Used by both worker roles
// once the large lane has closed so no small repo is left unprocessed.
func (sc *snapshotScheduler) drainSmall(workerID int) {
	for repo := range sc.smallCh {
		if sc.workerCtx.Err() != nil {
			return
		}
		sc.processSmall(repo, workerID)
	}
}

// drainLarge consumes the large lane to completion. Used by the small-preferring
// worker once the small lane has closed so no giant repo is left unprocessed.
// Each repo acquires the semaphore before processing and releases it via the
// afterSnapshot callback.
func (sc *snapshotScheduler) drainLarge(workerID int) {
	for repo := range sc.largeCh {
		if sc.workerCtx.Err() != nil {
			return
		}
		semAcquiredAt := time.Now()
		if !sc.acquireForLarge(repo, workerID) {
			return // ctx cancelled; semaphore never acquired — no release needed.
		}
		sc.processLargeHeld(repo, workerID, semAcquiredAt)
	}
}

// runSmallPreferring is the original small-preferring worker loop: it always
// tries the small lane first and only reaches for a giant (under the semaphore)
// when no small repo is immediately available. This keeps small repos flowing
// at full parallelism even while giants hold the semaphore.
func (sc *snapshotScheduler) runSmallPreferring(workerID int) {
	for {
		if sc.workerCtx.Err() != nil {
			return
		}

		// Priority: always try small repos first (non-blocking).
		select {
		case repo, ok := <-sc.smallCh:
			if !ok {
				sc.drainLarge(workerID)
				return
			}
			sc.processSmall(repo, workerID)
			continue
		default:
		}

		// No small repo immediately available. Try to acquire the large-repo
		// semaphore alongside checking for small repos. A worker NEVER blocks
		// waiting for the semaphore — if it's full, the largeSem case doesn't
		// fire and the worker handles small repos or waits for either channel.
		select {
		case repo, ok := <-sc.smallCh:
			if !ok {
				sc.drainLarge(workerID)
				return
			}
			sc.processSmall(repo, workerID)
		case sc.largeSem <- struct{}{}:
			// Acquired semaphore — pull a large repo.
			semAcquiredAt := time.Now()
			select {
			case repo, ok := <-sc.largeCh:
				if !ok {
					<-sc.largeSem
					sc.drainSmall(workerID)
					return
				}
				if sc.source.Instruments != nil {
					sc.source.Instruments.LargeRepoSemaphoreWait.Record(sc.workerCtx, 0)
				}
				if sc.source.Logger != nil {
					sc.source.Logger.InfoContext(sc.workerCtx, "large repo semaphore acquired",
						slog.String("repo_path", repo.RepoPath),
						slog.Int("worker_id", workerID),
						slog.Float64("wait_seconds", 0),
					)
				}
				sc.processLargeHeld(repo, workerID, semAcquiredAt)
			case repo, ok := <-sc.smallCh:
				// Got a small repo while waiting for large — release sem.
				<-sc.largeSem
				if !ok {
					sc.drainLarge(workerID)
					return
				}
				sc.processSmall(repo, workerID)
			case <-sc.workerCtx.Done():
				<-sc.largeSem
				return
			}
		case <-sc.workerCtx.Done():
			return
		}
	}
}

// runLargePreferring is the dedicated large-lane worker loop. It reserves a
// large-repo semaphore slot and then blocks on the large lane so a giant always
// STARTS in the first scheduling window even when the small lane was front-loaded
// by the classifier.
//
// Determinism over Go's random select: a large-preferring worker that holds a
// slot blocks on the large lane (plus ctx) ONLY — it never races the small lane
// in the same select. Racing the small lane would let Go pick a slow small repo
// even when a giant was already queued or moments away, which is exactly the
// front-loading defect this loop fixes. Holding a slot while blocked is bounded:
// there are at most largeMaxConcurrent such workers, the remaining workers keep
// the small lane flowing at full parallelism, and the classifier closes the
// large lane when it finishes, which unblocks the wait so this worker drains the
// small lane to completion.
//
// Semaphore audit: the slot reserved by `largeSem <- struct{}{}` is released by
// processLargeHeld's afterSnapshot on the giant path, and by an explicit
// `<-largeSem` on the large-closed and ctx-cancelled paths. The reserve select
// itself acquires no slot on the ctx-cancelled branch, so there is nothing to
// release there.
func (sc *snapshotScheduler) runLargePreferring(workerID int) {
	for {
		if sc.workerCtx.Err() != nil {
			return
		}

		// Reserve a giant slot with PRIORITY over small work: try the semaphore
		// non-blocking first. The small lane is often front-loaded and always
		// ready, so racing it against the semaphore in one select would let Go
		// pick a slow small repo and delay the giant — the exact defect this loop
		// fixes. Reserving the slot first guarantees a free slot is used for a
		// giant before this worker ever touches the small lane.
		select {
		case sc.largeSem <- struct{}{}:
			// Slot held: block-prefer the large lane until a giant arrives or the
			// lane closes. Returns false when the worker should exit.
			if !sc.runHeldGiantSlot(workerID) {
				return
			}
			continue
		default:
		}

		// Semaphore is full (largeMaxConcurrent giants already parsing). Do small
		// work so the worker is not wasted, or drain the large lane if the small
		// lane has closed.
		select {
		case repo, ok := <-sc.smallCh:
			if !ok {
				sc.drainLarge(workerID)
				return
			}
			sc.processSmall(repo, workerID)
		case <-sc.workerCtx.Done():
			return
		}
	}
}

// runHeldGiantSlot runs with one large-repo semaphore slot already held. It
// blocks on the large lane (preferred) so a giant that arrives starts here
// rather than behind a slow front-loaded small repo. It returns false when the
// worker should exit (large lane closed, or ctx cancelled), true to continue the
// outer loop. Every return path releases the held slot exactly once: the giant
// path releases via processLargeHeld's afterSnapshot, the other paths release
// inline.
func (sc *snapshotScheduler) runHeldGiantSlot(workerID int) bool {
	semAcquiredAt := time.Now()
	select {
	case repo, ok := <-sc.largeCh:
		if !ok {
			// Large lane closed: release the unused slot, drain the small lane.
			<-sc.largeSem
			sc.drainSmall(workerID)
			return false
		}
		sc.logLargeAcquired(repo, workerID)
		sc.processLargeHeld(repo, workerID, semAcquiredAt)
		return true
	case <-sc.workerCtx.Done():
		<-sc.largeSem
		return false
	}
}

// logLargeAcquired records the zero-wait semaphore acquisition for a giant that
// was pulled while the slot was already held.
func (sc *snapshotScheduler) logLargeAcquired(repo SelectedRepository, workerID int) {
	if sc.source.Instruments != nil {
		sc.source.Instruments.LargeRepoSemaphoreWait.Record(sc.workerCtx, 0)
	}
	if sc.source.Logger != nil {
		sc.source.Logger.InfoContext(sc.workerCtx, "large repo semaphore acquired",
			slog.String("repo_path", repo.RepoPath),
			slog.Int("worker_id", workerID),
			slog.Float64("wait_seconds", 0),
		)
	}
}
