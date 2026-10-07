// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitForWaiters blocks until a call for key is in flight and n callers have
// joined it (Waiting is a join count).
func waitForWaiters[T any](t *testing.T, f *Flight[T], key string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.Waiting(key) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d callers joined the in-flight call", f.Waiting(key), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFlightSharesOneCallAcrossConcurrentCallers(t *testing.T) {
	t.Parallel()

	const callers = 50
	var (
		f       Flight[int]
		runs    atomic.Int32
		release = make(chan struct{})
		wg      sync.WaitGroup
		shared  atomic.Int32
	)
	results := make([]int, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, wasShared, err := f.Do(context.Background(), "active_work_summary", func() (int, error) {
				runs.Add(1)
				<-release
				return 42, nil
			})
			if err != nil {
				t.Errorf("Do() error = %v", err)
			}
			if wasShared {
				shared.Add(1)
			}
			results[i] = value
		}()
	}
	waitForWaiters(t, &f, "active_work_summary", callers-1)
	close(release)
	wg.Wait()

	if got := runs.Load(); got != 1 {
		t.Fatalf("the live statement ran %d times for %d concurrent callers, want exactly 1", got, callers)
	}
	if got := shared.Load(); got != callers-1 {
		t.Fatalf("%d callers shared the result, want %d", got, callers-1)
	}
	for i, value := range results {
		if value != 42 {
			t.Fatalf("caller %d got %d, want 42", i, value)
		}
	}
}

func TestFlightRunsAgainAfterTheCallFinishes(t *testing.T) {
	t.Parallel()

	var (
		f    Flight[int]
		runs atomic.Int32
	)
	for i := 0; i < 3; i++ {
		value, shared, err := f.Do(context.Background(), "k", func() (int, error) { return int(runs.Add(1)), nil })
		if err != nil || shared || value != i+1 {
			t.Fatalf("call %d = %d shared=%v err=%v; sequential calls never share", i, value, shared, err)
		}
	}
}

func TestFlightKeysDoNotShare(t *testing.T) {
	t.Parallel()

	var f Flight[string]
	release := make(chan struct{})
	done := make(chan string, 1)
	go func() {
		v, _, _ := f.Do(context.Background(), "a", func() (string, error) { <-release; return "a", nil })
		done <- v
	}()
	waitForWaiters(t, &f, "a", 0)
	got, shared, err := f.Do(context.Background(), "b", func() (string, error) { return "b", nil })
	close(release)
	if err != nil || shared || got != "b" {
		t.Fatalf("Do(b) = %q shared=%v err=%v, want its own call", got, shared, err)
	}
	if v := <-done; v != "a" {
		t.Fatalf("Do(a) = %q", v)
	}
}

func TestFlightFollowersRunTheirOwnCallWhenTheLeaderFails(t *testing.T) {
	t.Parallel()

	// A leader's error (its own cancelled context, for one) is not the
	// followers' answer: each follower runs its own statement.
	var (
		f       Flight[int]
		runs    atomic.Int32
		release = make(chan struct{})
		wg      sync.WaitGroup
	)
	boom := errors.New("leader failed")
	leaderDone := make(chan error, 1)
	go func() {
		_, _, err := f.Do(context.Background(), "k", func() (int, error) {
			runs.Add(1)
			<-release
			return 0, boom
		})
		leaderDone <- err
	}()
	waitForWaiters(t, &f, "k", 0)
	var followerValue int
	var followerErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		followerValue, _, followerErr = f.Do(context.Background(), "k", func() (int, error) {
			runs.Add(1)
			return 7, nil
		})
	}()
	waitForWaiters(t, &f, "k", 1)
	close(release)
	wg.Wait()
	if err := <-leaderDone; !errors.Is(err, boom) {
		t.Fatalf("leader error = %v, want %v", err, boom)
	}
	if followerErr != nil || followerValue != 7 {
		t.Fatalf("follower = %d, %v; want its own successful call", followerValue, followerErr)
	}
	if got := runs.Load(); got != 2 {
		t.Fatalf("calls = %d, want the leader's and the follower's own", got)
	}
}

func TestFlightReleasesFollowersWhenTheLeaderPanics(t *testing.T) {
	t.Parallel()

	var f Flight[int]
	release := make(chan struct{})
	leaderDone := make(chan any, 1)
	go func() {
		defer func() { leaderDone <- recover() }()
		_, _, _ = f.Do(context.Background(), "k", func() (int, error) { <-release; panic("boom") })
	}()
	waitForWaiters(t, &f, "k", 0)
	followerDone := make(chan int, 1)
	go func() {
		v, _, _ := f.Do(context.Background(), "k", func() (int, error) { return 9, nil })
		followerDone <- v
	}()
	waitForWaiters(t, &f, "k", 1)
	close(release)
	if got := <-leaderDone; got == nil {
		t.Fatal("the leader's panic was swallowed")
	}
	select {
	case v := <-followerDone:
		if v != 9 {
			t.Fatalf("follower = %d, want its own call after the leader panicked", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a follower hung after the leader panicked")
	}
}

func TestFlightFollowerStopsWaitingWhenItsOwnContextEnds(t *testing.T) {
	t.Parallel()

	// A follower whose request is cancelled must not hold its transaction
	// until the leader finishes; the leader keeps running for the others.
	var (
		f       Flight[int]
		runs    atomic.Int32
		release = make(chan struct{})
	)
	leaderDone := make(chan int, 1)
	go func() {
		v, _, _ := f.Do(context.Background(), "k", func() (int, error) {
			runs.Add(1)
			<-release
			return 5, nil
		})
		leaderDone <- v
	}()
	waitForWaiters(t, &f, "k", 0)

	ctx, cancel := context.WithCancel(context.Background())
	followerErr := make(chan error, 1)
	go func() {
		_, shared, err := f.Do(ctx, "k", func() (int, error) { runs.Add(1); return 99, nil })
		if shared {
			t.Error("a cancelled follower reported a shared result")
		}
		followerErr <- err
	}()
	waitForWaiters(t, &f, "k", 1)
	cancel()
	select {
	case err := <-followerErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("follower error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled follower kept waiting for the leader")
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("calls = %d, want only the leader's: a cancelled follower runs nothing", got)
	}
	close(release)
	if v := <-leaderDone; v != 5 {
		t.Fatalf("leader = %d, want 5", v)
	}
}

// TestFlightWaitingCountsJoinersUntilTheLeaderEnds pins what Waiting reports: a
// join count. A follower that cancels after joining has left, but it stays
// counted until the leader ends. The tests that use Waiting as a barrier rely
// on it never decreasing, so a decrement on the cancel path must fail here.
func TestFlightWaitingCountsJoinersUntilTheLeaderEnds(t *testing.T) {
	t.Parallel()

	var (
		f       Flight[int]
		release = make(chan struct{})
	)
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		_, _, _ = f.Do(context.Background(), "k", func() (int, error) { <-release; return 1, nil })
	}()
	waitForWaiters(t, &f, "k", 0)
	if got := f.Waiting("k"); got != 0 {
		t.Fatalf("Waiting() with only the leader = %d, want 0", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	followerDone := make(chan error, 1)
	go func() {
		_, _, err := f.Do(ctx, "k", func() (int, error) { return 2, nil })
		followerDone <- err
	}()
	waitForWaiters(t, &f, "k", 1)
	cancel()
	if err := <-followerDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("follower error = %v, want context.Canceled", err)
	}
	if got := f.Waiting("k"); got != 1 {
		t.Fatalf("Waiting() after the follower left = %d, want 1: it counts joiners until the leader ends", got)
	}

	close(release)
	<-leaderDone
	if got := f.Waiting("k"); got != -1 {
		t.Fatalf("Waiting() after the leader ended = %d, want -1", got)
	}
}
