// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "sync"

// recentFindsCapacity bounds how many sessions' last find_tool results the
// engine keeps. Eviction is first-in, first-out by a session's first
// find_tool (a repeat find_tool does not refresh its place), so an evicted
// session's later dispatched call_tool records rank 0 (not offered).
const recentFindsCapacity = 1024

// recentFinds remembers each session's last find_tool candidate names, so a
// call_tool can report where its target ranked. Memory is bounded by
// capacity sessions times MaxLimit names.
type recentFinds struct {
	mu       sync.Mutex
	capacity int
	order    []string
	names    map[string][]string
}

func newRecentFinds(capacity int) *recentFinds {
	return &recentFinds{capacity: capacity, names: make(map[string][]string, capacity)}
}

// remember stores names as session's latest find_tool result.
func (r *recentFinds) remember(session string, names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.names[session]; !ok {
		if len(r.order) >= r.capacity {
			oldest := r.order[0]
			r.order = r.order[1:]
			delete(r.names, oldest)
		}
		r.order = append(r.order, session)
	}
	r.names[session] = append([]string(nil), names...)
}

// rank returns the 1-based position of target in session's last find_tool
// result, or 0 when it was not offered or the session has no result.
func (r *recentFinds) rank(session, target string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, name := range r.names[session] {
		if name == target {
			return i + 1
		}
	}
	return 0
}
