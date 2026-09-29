// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

// restrictToCyclicComponents narrows a file digraph to the hops that can lie on
// a cycle. Every simple cycle lies inside one strongly connected component, so a
// hop whose endpoints sit in different components cannot be part of any cycle,
// and a node alone in its component (with no self loop, which the graph builder
// never emits) cannot start one. Walking only the surviving hops leaves the set
// of cycles unchanged and makes the walk cost depend on the cyclic part of the
// graph: an acyclic graph, however dense, leaves nothing to walk.
//
// It returns the nodes that belong to a component of two or more files, in the
// input order, and an adjacency holding only same-component hops.
func restrictToCyclicComponents(
	nodes []string,
	adjacency map[string][]importCycleHop,
) ([]string, map[string][]importCycleHop) {
	component, size := stronglyConnectedComponents(nodes, adjacency)

	cyclicNodes := make([]string, 0, len(nodes))
	restricted := make(map[string][]importCycleHop, len(nodes))
	for _, node := range nodes {
		if size[component[node]] < 2 {
			continue
		}
		cyclicNodes = append(cyclicNodes, node)
		for _, hop := range adjacency[node] {
			if component[hop.destination] == component[node] {
				restricted[node] = append(restricted[node], hop)
			}
		}
	}
	return cyclicNodes, restricted
}

// stronglyConnectedComponents labels every node with its component using
// Tarjan's algorithm, and reports each component's size. The recursion depth is
// bounded by the node count, which the 25,000-row scan limit already bounds.
func stronglyConnectedComponents(
	nodes []string,
	adjacency map[string][]importCycleHop,
) (map[string]int, map[int]int) {
	const unvisited = -1
	index := make(map[string]int, len(nodes))
	lowlink := make(map[string]int, len(nodes))
	onStack := make(map[string]bool, len(nodes))
	component := make(map[string]int, len(nodes))
	size := make(map[int]int)
	for _, node := range nodes {
		index[node] = unvisited
	}

	var stack []string
	next, components := 0, 0
	var connect func(node string)
	connect = func(node string) {
		index[node] = next
		lowlink[node] = next
		next++
		stack = append(stack, node)
		onStack[node] = true

		for _, hop := range adjacency[node] {
			destination := hop.destination
			switch {
			case index[destination] == unvisited:
				connect(destination)
				lowlink[node] = min(lowlink[node], lowlink[destination])
			case onStack[destination]:
				lowlink[node] = min(lowlink[node], index[destination])
			}
		}

		if lowlink[node] != index[node] {
			return
		}
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			component[top] = components
			size[components]++
			if top == node {
				break
			}
		}
		components++
	}
	for _, node := range nodes {
		if index[node] == unvisited {
			connect(node)
		}
	}
	return component, size
}
