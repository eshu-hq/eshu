// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import "sort"

// condense computes the strongly-connected components of the current call graph
// and returns the functions in reverse-topological order (every callee before
// the callers that depend on it) along with a map from function to SCC index.
// Mutually recursive functions share an SCC, which lets recompute exclude
// same-SCC callee versions from the content hash and terminate.
//
// Implemented with an iterative Tarjan, which emits SCCs in the required
// reverse-topological order without recursion, so a deep call chain cannot
// overflow the stack. Node and edge iteration is sorted, so the order is
// deterministic.
func (s *Store) condense() (order []FunctionID, sccID map[FunctionID]int) {
	nodes := make([]FunctionID, 0, len(s.entries))
	for id := range s.entries {
		nodes = append(nodes, id)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })

	t := &tarjan{
		store:   s,
		index:   make(map[FunctionID]int, len(nodes)),
		low:     make(map[FunctionID]int, len(nodes)),
		onStack: make(map[FunctionID]bool, len(nodes)),
		sccID:   make(map[FunctionID]int, len(nodes)),
	}
	for _, v := range nodes {
		if _, seen := t.index[v]; !seen {
			t.run(v)
		}
	}
	return t.order, t.sccID
}

// tarjan carries the bookkeeping for one iterative condense run.
type tarjan struct {
	store   *Store
	index   map[FunctionID]int
	low     map[FunctionID]int
	onStack map[FunctionID]bool
	stack   []FunctionID
	counter int
	nextSCC int
	order   []FunctionID
	sccID   map[FunctionID]int
}

// frame is one node's resumable DFS state on the explicit work stack.
type frame struct {
	v       FunctionID
	callees []FunctionID
	ci      int // index of the next callee to visit
}

// run performs an iterative depth-first Tarjan from start.
func (t *tarjan) run(start FunctionID) {
	frames := []frame{t.push(start)}
	for len(frames) > 0 {
		idx := len(frames) - 1
		if frames[idx].ci < len(frames[idx].callees) {
			w := frames[idx].callees[frames[idx].ci]
			frames[idx].ci++
			if _, exists := t.store.entries[w]; !exists {
				continue
			}
			if _, seen := t.index[w]; !seen {
				frames = append(frames, t.push(w))
				continue
			}
			if t.onStack[w] && t.index[w] < t.low[frames[idx].v] {
				t.low[frames[idx].v] = t.index[w]
			}
			continue
		}

		v := frames[idx].v
		if t.low[v] == t.index[v] {
			t.popComponent(v)
		}
		frames = frames[:idx]
		if idx > 0 && t.low[v] < t.low[frames[idx-1].v] {
			t.low[frames[idx-1].v] = t.low[v]
		}
	}
}

// push assigns DFS numbers to v, places it on the SCC stack, and returns its
// work frame.
func (t *tarjan) push(v FunctionID) frame {
	t.index[v] = t.counter
	t.low[v] = t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.onStack[v] = true
	var callees []FunctionID
	if e := t.store.entries[v]; e != nil {
		callees = e.callees
	}
	return frame{v: v, callees: callees}
}

// popComponent pops the strongly-connected component rooted at v.
func (t *tarjan) popComponent(v FunctionID) {
	var component []FunctionID
	for {
		w := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.onStack[w] = false
		component = append(component, w)
		t.sccID[w] = t.nextSCC
		if w == v {
			break
		}
	}
	sort.Slice(component, func(i, j int) bool { return component[i] < component[j] })
	t.order = append(t.order, component...)
	t.nextSCC++
}
