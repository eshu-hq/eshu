// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitAllocatorQueue(t *testing.T, allocator *readerAllocator, member, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		allocator.mu.Lock()
		got := len(allocator.waiting[member])
		allocator.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("member %d queue did not reach %d waiters", member, want)
}

func TestReaderAllocatorProtectsFourAndAllowsOtherMember(t *testing.T) {
	allocator := newReaderAllocator([]int{4, 4}, 8)
	holds := make([]*readerReservation, 0, 3)
	for _, count := range []int{1, 1, 1} {
		lease, err := allocator.reserve(context.Background(), []int{0}, count)
		if err != nil {
			t.Fatal(err)
		}
		holds = append(holds, lease)
	}
	fourReady := make(chan *readerReservation, 1)
	go func() {
		lease, err := allocator.reserve(context.Background(), []int{0}, 4)
		if err == nil {
			fourReady <- lease
		}
	}()
	waitAllocatorQueue(t, allocator, 0, 1)
	oneReady := make(chan *readerReservation, 1)
	go func() {
		lease, err := allocator.reserve(context.Background(), []int{0}, 1)
		if err == nil {
			oneReady <- lease
		}
	}()
	waitAllocatorQueue(t, allocator, 0, 2)
	peer, err := allocator.reserve(context.Background(), []int{1}, 4)
	if err != nil {
		t.Fatal(err)
	}
	peer.Release()
	select {
	case lease := <-oneReady:
		lease.Release()
		t.Fatal("later one-slot borrower bypassed protected four-slot set")
	default:
	}
	for _, hold := range holds {
		hold.Release()
	}
	select {
	case lease := <-fourReady:
		select {
		case later := <-oneReady:
			later.Release()
			t.Fatal("later single bypassed granted four-slot set")
		default:
		}
		lease.Release()
	case <-time.After(time.Second):
		t.Fatal("protected four-slot set did not wake")
	}
	select {
	case lease := <-oneReady:
		lease.Release()
	case <-time.After(time.Second):
		t.Fatal("single did not wake after protected set released")
	}
	allocator.mu.Lock()
	used, queued := allocator.total, len(allocator.waiting[0])
	allocator.mu.Unlock()
	if used != 0 || queued != 0 {
		t.Fatalf("reservation leak used=%d queued=%d", used, queued)
	}
}

func TestReaderAllocatorReassignsWaitingSetToFreedPeer(t *testing.T) {
	allocator := newReaderAllocator([]int{4, 4}, 8)
	first, err := allocator.reserve(context.Background(), []int{0}, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := allocator.reserve(context.Background(), []int{1}, 4)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan *readerReservation, 1)
	go func() {
		lease, reserveErr := allocator.reserve(context.Background(), []int{0, 1}, 4)
		if reserveErr == nil {
			ready <- lease
		}
	}()
	waitAllocatorQueue(t, allocator, 0, 1)
	second.Release()
	select {
	case lease := <-ready:
		if lease.member != 1 {
			t.Fatalf("reassigned member=%d, want idle B", lease.member)
		}
		lease.Release()
	case <-time.After(150 * time.Millisecond):
		t.Fatal("waiting set did not reassign to freed peer")
	}
}

func TestReaderAllocatorCancelReleaseRaceDoesNotLeak(t *testing.T) {
	for range 100 {
		allocator := newReaderAllocator([]int{4, 4}, 8)
		hold, err := allocator.reserve(context.Background(), []int{0}, 4)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			lease, reserveErr := allocator.reserve(ctx, []int{0}, 4)
			if lease != nil {
				lease.Release()
			}
			done <- reserveErr
		}()
		waitAllocatorQueue(t, allocator, 0, 1)
		cancel()
		hold.Release()
		if reserveErr := <-done; reserveErr != nil && !errors.Is(reserveErr, context.Canceled) {
			t.Fatalf("cancel/release result=%v", reserveErr)
		}
		allocator.mu.Lock()
		used, queued := allocator.total, len(allocator.waiting[0])
		allocator.mu.Unlock()
		if used != 0 || queued != 0 {
			t.Fatalf("cancel/release leaked used=%d queued=%d", used, queued)
		}
	}
}
