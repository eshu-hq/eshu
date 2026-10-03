// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"
)

// readerAllocator atomically reserves whole requests against both the physical
// member and aggregate reader budgets. No network or database I/O holds mu.
type readerAllocator struct {
	mu       sync.Mutex
	capacity []int
	used     []int
	total    int
	maximum  int
	waiting  [][]*readerWaiter
	nextSeq  uint64
}

type readerWaiter struct {
	count   int
	allowed []int
	member  int
	seq     uint64
	ready   chan struct{}
	granted bool
}

type readerReservation struct {
	allocator *readerAllocator
	member    int
	count     int
	remaining int // guarded by allocator.mu
}

func newReaderReservation(allocator *readerAllocator, member, count int) *readerReservation {
	return &readerReservation{allocator: allocator, member: member, count: count, remaining: count}
}

func newReaderAllocator(capacities []int, maximum int) *readerAllocator {
	return &readerAllocator{
		capacity: append([]int(nil), capacities...),
		used:     make([]int, len(capacities)),
		maximum:  maximum,
		waiting:  make([][]*readerWaiter, len(capacities)),
	}
}

func (a *readerAllocator) canGrantLocked(member, count int) bool {
	return a.used[member]+count <= a.capacity[member] && a.total+count <= a.maximum
}

func (a *readerAllocator) grantLocked(waiter *readerWaiter, member int) {
	a.used[member] += waiter.count
	a.total += waiter.count
	waiter.member = member
	waiter.granted = true
	close(waiter.ready)
}

func (a *readerAllocator) removeWaiterLocked(waiter *readerWaiter) {
	for member, queue := range a.waiting {
		for index, candidate := range queue {
			if candidate == waiter {
				a.waiting[member] = append(queue[:index], queue[index+1:]...)
				return
			}
		}
	}
}

func (a *readerAllocator) scheduleLocked() {
	for {
		progress := false
		// First serve each member's own protected FIFO head. A later N=1
		// cannot take a slot reserved for the oldest waiting N=4.
		for member := range a.capacity {
			for len(a.waiting[member]) > 0 {
				waiter := a.waiting[member][0]
				if !a.canGrantLocked(member, waiter.count) {
					break
				}
				a.waiting[member] = a.waiting[member][1:]
				a.grantLocked(waiter, member)
				progress = true
			}
		}
		// A free peer can take the oldest eligible head from another
		// member. Reassignment removes it from the original FIFO atomically.
		for member := range a.capacity {
			if len(a.waiting[member]) > 0 {
				continue
			}
			var oldest *readerWaiter
			for original, queue := range a.waiting {
				if original == member || len(queue) == 0 {
					continue
				}
				candidate := queue[0]
				if !a.canGrantLocked(member, candidate.count) || !containsMember(candidate.allowed, member) {
					continue
				}
				if oldest == nil || candidate.seq < oldest.seq {
					oldest = candidate
				}
			}
			if oldest != nil {
				a.removeWaiterLocked(oldest)
				a.grantLocked(oldest, member)
				progress = true
			}
		}
		if !progress {
			return
		}
	}
}

func containsMember(members []int, member int) bool {
	for _, candidate := range members {
		if candidate == member {
			return true
		}
	}
	return false
}

func (a *readerAllocator) reserve(ctx context.Context, order []int, count int) (*readerReservation, error) {
	if count < 1 || len(order) == 0 {
		return nil, errors.New("invalid reader reservation")
	}
	a.mu.Lock()
	if err := ctx.Err(); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	eligible := make([]int, 0, len(order))
	for _, member := range order {
		if member >= 0 && member < len(a.capacity) && a.capacity[member] >= count && !containsMember(eligible, member) {
			eligible = append(eligible, member)
		}
	}
	if len(eligible) == 0 {
		a.mu.Unlock()
		return nil, errors.New("no reader member fits reservation")
	}
	for _, member := range eligible {
		if len(a.waiting[member]) == 0 && a.canGrantLocked(member, count) {
			a.used[member] += count
			a.total += count
			a.mu.Unlock()
			return newReaderReservation(a, member, count), nil
		}
	}
	waiter := &readerWaiter{count: count, allowed: eligible, member: eligible[0], seq: a.nextSeq, ready: make(chan struct{})}
	a.nextSeq++
	a.waiting[waiter.member] = append(a.waiting[waiter.member], waiter)
	a.mu.Unlock()
	select {
	case <-waiter.ready:
		lease := newReaderReservation(a, waiter.member, count)
		if err := ctx.Err(); err != nil {
			lease.Release()
			return nil, err
		}
		return lease, nil
	case <-ctx.Done():
		a.mu.Lock()
		if waiter.granted {
			a.used[waiter.member] -= count
			a.total -= count
		} else {
			a.removeWaiterLocked(waiter)
		}
		a.scheduleLocked()
		a.mu.Unlock()
		return nil, ctx.Err()
	}
}

// ReleaseOne returns one reserved connection slot after that physical
// connection is closed. Concurrent cancellation and explicit close are safe.
func (r *readerReservation) ReleaseOne() {
	if r == nil {
		return
	}
	a := r.allocator
	a.mu.Lock()
	if r.remaining > 0 {
		r.remaining--
		a.used[r.member]--
		a.total--
		a.scheduleLocked()
	}
	a.mu.Unlock()
}

// Release returns every remaining slot after failed setup, or an unsplit
// single-use reservation. It is idempotent across concurrent close paths.
func (r *readerReservation) Release() {
	if r == nil {
		return
	}
	a := r.allocator
	a.mu.Lock()
	if r.remaining > 0 {
		a.used[r.member] -= r.remaining
		a.total -= r.remaining
		r.remaining = 0
		a.scheduleLocked()
	}
	a.mu.Unlock()
}
