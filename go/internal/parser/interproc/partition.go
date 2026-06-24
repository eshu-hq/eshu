// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package interproc

import "sort"

// partition splits a program into weakly-connected components: groups of ports
// reachable from each other when edges are treated as undirected. Taint cannot
// cross a component boundary (it only flows along edges), so each component can
// be solved independently and in parallel. The conflict key of a sub-program is
// its component, which is why concurrent solving is race-free without
// serialization.
func partition(program Program) []Program {
	uf := newUnionFind()
	for _, edge := range program.Edges {
		uf.union(edge.From, edge.To)
	}
	for _, src := range program.Sources {
		uf.add(src.Port)
	}
	for _, sink := range program.Sinks {
		uf.add(sink.Port)
	}
	for _, san := range program.Sanitizers {
		uf.add(san.Port)
	}

	byRoot := map[Port]*Program{}
	roots := []Port{}
	get := func(port Port) *Program {
		root := uf.find(port)
		prog := byRoot[root]
		if prog == nil {
			prog = &Program{}
			byRoot[root] = prog
			roots = append(roots, root)
		}
		return prog
	}
	for _, edge := range program.Edges {
		p := get(edge.From)
		p.Edges = append(p.Edges, edge)
	}
	for _, src := range program.Sources {
		p := get(src.Port)
		p.Sources = append(p.Sources, src)
	}
	for _, sink := range program.Sinks {
		p := get(sink.Port)
		p.Sinks = append(p.Sinks, sink)
	}
	for _, san := range program.Sanitizers {
		p := get(san.Port)
		p.Sanitizers = append(p.Sanitizers, san)
	}

	// Deterministic component order (does not affect findings, which are sorted).
	sort.Slice(roots, func(i, j int) bool { return portLess(roots[i], roots[j]) })
	out := make([]Program, 0, len(roots))
	for _, root := range roots {
		out = append(out, *byRoot[root])
	}
	return out
}

// unionFind is a disjoint-set structure over ports with path compression and
// union by size.
type unionFind struct {
	parent map[Port]Port
	size   map[Port]int
}

func newUnionFind() *unionFind {
	return &unionFind{parent: map[Port]Port{}, size: map[Port]int{}}
}

func (u *unionFind) add(port Port) {
	if _, ok := u.parent[port]; !ok {
		u.parent[port] = port
		u.size[port] = 1
	}
}

func (u *unionFind) find(port Port) Port {
	u.add(port)
	for u.parent[port] != port {
		u.parent[port] = u.parent[u.parent[port]]
		port = u.parent[port]
	}
	return port
}

func (u *unionFind) union(a, b Port) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.size[ra] < u.size[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	u.size[ra] += u.size[rb]
}
