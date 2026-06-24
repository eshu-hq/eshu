// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cfg

import "sort"

// computeControlDependencies derives block-level control dependence using the
// standard post-dominator frontier over the already-built CFG.
func computeControlDependencies(blocks []Block, maxEdges int) ([]ControlDependence, int) {
	if len(blocks) == 0 {
		return nil, 0
	}
	succs, syntheticExit := augmentedSuccessors(blocks)
	postdom := postDominatorSets(succs)
	ipdom := immediatePostDominators(postdom)
	guards := guardStatementsByBlock(blocks)

	type edgeKey struct {
		guardStmt int
		block     int
		guard     string
	}
	seen := map[edgeKey]struct{}{}
	var out []ControlDependence
	generated := 0
	for _, block := range blocks {
		guard, ok := guards[block.ID]
		if !ok {
			continue
		}
		stop := ipdom[block.ID]
		for _, succ := range block.Succs {
			if containsInt(postdom[block.ID], succ) {
				continue
			}
			edgeGuard := guard.Guard
			if block.SuccGuards != nil && block.SuccGuards[succ] != "" {
				edgeGuard = block.SuccGuards[succ]
			}
			for runner, steps := succ, 0; runner >= 0 && runner != stop && steps <= len(ipdom); runner, steps = ipdom[runner], steps+1 {
				if runner == syntheticExit || runner == block.ID {
					continue
				}
				key := edgeKey{guardStmt: guard.ID, block: runner, guard: edgeGuard}
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				generated++
				if len(out) >= maxEdges {
					continue
				}
				out = append(out, ControlDependence{
					GuardBlock:     block.ID,
					GuardStmt:      guard.ID,
					GuardLine:      guard.Line,
					Guard:          edgeGuard,
					DependentBlock: runner,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DependentBlock != out[j].DependentBlock {
			return out[i].DependentBlock < out[j].DependentBlock
		}
		if out[i].GuardStmt != out[j].GuardStmt {
			return out[i].GuardStmt < out[j].GuardStmt
		}
		return out[i].Guard < out[j].Guard
	})
	return out, generated - len(out)
}

func guardStatementsByBlock(blocks []Block) map[int]Stmt {
	guards := map[int]Stmt{}
	for _, block := range blocks {
		for _, stmt := range block.Stmts {
			if stmt.Guard != "" {
				guards[block.ID] = stmt
			}
		}
	}
	return guards
}

func augmentedSuccessors(blocks []Block) ([][]int, int) {
	syntheticExit := len(blocks)
	succs := make([][]int, len(blocks)+1)
	for _, block := range blocks {
		if len(block.Succs) == 0 {
			succs[block.ID] = []int{syntheticExit}
			continue
		}
		succs[block.ID] = append([]int(nil), block.Succs...)
	}
	return succs, syntheticExit
}

func postDominatorSets(succs [][]int) []map[int]struct{} {
	universe := map[int]struct{}{}
	for id := range succs {
		universe[id] = struct{}{}
	}
	sets := make([]map[int]struct{}, len(succs))
	for id, successors := range succs {
		if len(successors) == 0 {
			sets[id] = map[int]struct{}{id: {}}
			continue
		}
		sets[id] = cloneIntSet(universe)
	}

	changed := true
	for changed {
		changed = false
		for id := len(succs) - 1; id >= 0; id-- {
			successors := succs[id]
			if len(successors) == 0 {
				continue
			}
			next := cloneIntSet(sets[successors[0]])
			for _, succ := range successors[1:] {
				intersectIntSet(next, sets[succ])
			}
			next[id] = struct{}{}
			if !intSetEqual(next, sets[id]) {
				sets[id] = next
				changed = true
			}
		}
	}
	return sets
}

func immediatePostDominators(postdom []map[int]struct{}) []int {
	ipdom := make([]int, len(postdom))
	for i := range ipdom {
		ipdom[i] = -1
	}
	for block, set := range postdom {
		var candidates []int
		for candidate := range set {
			if candidate != block {
				candidates = append(candidates, candidate)
			}
		}
		sort.Ints(candidates)
		for _, candidate := range candidates {
			nearest := true
			for _, other := range candidates {
				if other == candidate {
					continue
				}
				if !containsInt(postdom[candidate], other) {
					nearest = false
					break
				}
			}
			if nearest {
				ipdom[block] = candidate
				break
			}
		}
	}
	return ipdom
}

func cloneIntSet(in map[int]struct{}) map[int]struct{} {
	out := make(map[int]struct{}, len(in))
	for value := range in {
		out[value] = struct{}{}
	}
	return out
}

func intersectIntSet(dst map[int]struct{}, other map[int]struct{}) {
	for value := range dst {
		if _, ok := other[value]; !ok {
			delete(dst, value)
		}
	}
}

func intSetEqual(a, b map[int]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for value := range a {
		if _, ok := b[value]; !ok {
			return false
		}
	}
	return true
}

func containsInt(set map[int]struct{}, value int) bool {
	_, ok := set[value]
	return ok
}
