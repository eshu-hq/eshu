// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	// importCycleDefaultMaxLength is the default simple-cycle length bound
	// (#6851): cycles longer than this are not enumerated unless the
	// caller raises max_cycle_length.
	importCycleDefaultMaxLength = 5
	// importCycleMinMaxLength and importCycleMaxMaxLength bound an
	// explicit max_cycle_length. The ceiling keeps depth-first
	// enumeration bounded on dense graphs alongside the enumeration cap.
	importCycleMinMaxLength = 2
	importCycleMaxMaxLength = 8
)

// importCycleEnumerationCap bounds file_import_cycles simple-cycle
// enumeration (#6851). It sits far above the 200-row page ceiling, so a
// capped enumeration always pages with truncated:true: the response is
// never a silently partial list. It sits far below the 25,000-row
// internal scan limit, so the in-process depth-first walk stays bounded
// even on the largest measured corpus (38,240 IMPORTS edges).
const importCycleEnumerationCap = 1000

// importCycleStep is one directed hop of an enumerated simple cycle: the
// importing file, the module it declares, the imported module naming the
// next file, and the earliest positive source line for the pair.
type importCycleStep struct {
	repoID       string
	repoName     string
	file         string
	sourceModule string
	targetModule string
	lineNumber   int
}

// importCycle is one enumerated simple cycle. Steps are ordered along the
// import direction and start at the cycle's lexicographically smallest
// (repository, file) member, so rotations of one directed cycle share a
// single canonical form. The closing edge back to steps[0] is steps[len-1].
type importCycle struct {
	steps []importCycleStep
}

// importCycleNodeMeta carries one collapsed-graph node's file identity:
// the repository, the file, and the module the file declares.
type importCycleNodeMeta struct {
	repoID       string
	repoName     string
	file         string
	sourceModule string
}

// importCycleHop is one collapsed file-to-file import hop: the
// destination node plus the proof carried on the underlying import edge.
type importCycleHop struct {
	destination  string
	targetModule string
	lineNumber   int
}

// enumerateImportCycles collapses deduplicated import edges into a
// file-level digraph and enumerates its simple cycles up to maxLength.
// Nodes are (repository, file) pairs; an edge runs from an importing file
// to every same-repository file whose declared module exactly equals the
// imported module name. Only Python edges reach this function today: the
// request validation still rejects every other explicit language, and the
// edge fetch is Python-scoped.
//
// The walk starts a depth-first search from every file in sorted order
// and only visits files larger than the start, so the start is always the
// cycle's smallest member and each rotation class is found once. Cycles
// read length-ascending, then normalized path, so the output order is
// deterministic across runs. Enumeration stops at
// importCycleEnumerationCap and reports truncated; the response carries
// the cap value, so a capped list is never silently partial.
//
// Why here and not in Cypher: the same edge fetch profiles at 40ms/27k
// DbHits for 4,522 rows while a Cypher reciprocal join over it costs
// 1,522ms/5.5M DbHits with zero rows, and variable-length cycle patterns
// scale worse. The projector now stores type_only, deferred, and inferred on
// IMPORTS edges (#7345), but this reader does not fetch or consume them yet, so
// cycles are still computed over all stored IMPORTS edges with no type-only or
// deferred exclusion and no inferred labelling; #7346 owns consuming them.
func enumerateImportCycles(edges []importCycleEdge, maxLength int) ([]importCycle, CycleEnumeration) {
	return enumerateImportCyclesWithinBudget(edges, maxLength, importCycleEnumerationStepBudget)
}

// enumerateImportCyclesWithinBudget is enumerateImportCycles with the step
// budget injected, so tests can exhaust it on a small graph.
func enumerateImportCyclesWithinBudget(edges []importCycleEdge, maxLength, stepBudget int) ([]importCycle, CycleEnumeration) {
	allNodes, allAdjacency, meta := buildImportCycleGraph(edges)
	// Only hops inside a strongly connected component can lie on a cycle, so the
	// walk starts from and stays within those; see restrictToCyclicComponents.
	nodes, adjacency := restrictToCyclicComponents(allNodes, allAdjacency)
	var cycles []importCycle
	seen := make(map[string]struct{})
	truncated := false
	steps := 0
	stopReason := CycleStopNone
	for _, start := range nodes {
		if truncated {
			break
		}
		onStack := map[string]bool{start: true}
		var path []string
		var proofs []importCycleHop
		var visit func(current string)
		visit = func(current string) {
			if truncated {
				return
			}
			for _, hop := range adjacency[current] {
				if truncated {
					return
				}
				if steps >= stepBudget {
					truncated = true
					stopReason = CycleStopStepBudget
					return
				}
				steps++
				if hop.destination == start {
					if len(path)+1 < 2 || len(path)+1 > maxLength {
						continue
					}
					cycle := closeImportCycle(start, path, proofs, hop, meta)
					key := importCycleKey(cycle)
					if _, exists := seen[key]; exists {
						continue
					}
					seen[key] = struct{}{}
					cycles = append(cycles, cycle)
					if len(cycles) >= importCycleEnumerationCap {
						truncated = true
						stopReason = CycleStopCycleCap
						return
					}
					continue
				}
				if hop.destination <= start || onStack[hop.destination] {
					continue
				}
				if len(path)+1 >= maxLength {
					continue
				}
				onStack[hop.destination] = true
				path = append(path, hop.destination)
				proofs = append(proofs, hop)
				visit(hop.destination)
				path = path[:len(path)-1]
				proofs = proofs[:len(proofs)-1]
				delete(onStack, hop.destination)
			}
		}
		visit(start)
	}
	sort.Slice(cycles, func(i, j int) bool {
		return compareImportCycles(cycles[i], cycles[j]) < 0
	})
	return cycles, CycleEnumeration{
		Truncated:     truncated,
		StopReason:    stopReason,
		StepBudget:    stepBudget,
		StepsExamined: steps,
	}
}

// buildImportCycleGraph collapses import edges into a sorted file-level
// digraph. The destination of one edge is every same-repository file
// declaring the imported module; duplicate (source, destination) pairs
// keep the earliest positive line. Nodes sort by repository, then file,
// so enumeration order is deterministic.
func buildImportCycleGraph(
	edges []importCycleEdge,
) ([]string, map[string][]importCycleHop, map[string]importCycleNodeMeta) {
	meta := make(map[string]importCycleNodeMeta, len(edges))
	for _, edge := range edges {
		key := importCycleNodeKey(edge.repoID, edge.sourceFile)
		if _, exists := meta[key]; exists {
			continue
		}
		meta[key] = importCycleNodeMeta{
			repoID:       edge.repoID,
			repoName:     edge.repoName,
			file:         edge.sourceFile,
			sourceModule: edge.sourceModule,
		}
	}
	byModule := make(map[string][]string)
	for key, node := range meta {
		moduleKey := node.repoID + "\x00" + node.sourceModule
		byModule[moduleKey] = append(byModule[moduleKey], key)
	}
	for _, keys := range byModule {
		sort.Strings(keys)
	}
	type hopPair struct {
		source      string
		destination string
	}
	hops := make(map[hopPair]importCycleHop)
	for _, edge := range edges {
		source := importCycleNodeKey(edge.repoID, edge.sourceFile)
		for _, destination := range byModule[edge.repoID+"\x00"+edge.targetModule] {
			if destination == source {
				continue
			}
			pair := hopPair{source: source, destination: destination}
			candidate := importCycleHop{
				destination:  destination,
				targetModule: edge.targetModule,
				lineNumber:   edge.lineNumber,
			}
			current, exists := hops[pair]
			if !exists || earlierImportCycleHop(candidate, current) {
				hops[pair] = candidate
			}
		}
	}
	adjacency := make(map[string][]importCycleHop, len(meta))
	for pair, hop := range hops {
		adjacency[pair.source] = append(adjacency[pair.source], hop)
	}
	nodes := make([]string, 0, len(meta))
	for key := range meta {
		nodes = append(nodes, key)
	}
	sort.Strings(nodes)
	for _, key := range nodes {
		hopList := adjacency[key]
		sort.Slice(hopList, func(i, j int) bool {
			return hopList[i].destination < hopList[j].destination
		})
		adjacency[key] = hopList
	}
	return nodes, adjacency, meta
}

// closeImportCycle materializes one enumerated simple cycle: start is the
// search start (the closing hop's destination), path holds the visited
// nodes after it, proofs holds the hop taken to reach each of them, and
// closing is the hop back to the start.
func closeImportCycle(
	start string,
	path []string,
	proofs []importCycleHop,
	closing importCycleHop,
	meta map[string]importCycleNodeMeta,
) importCycle {
	members := make([]string, 0, len(path)+1)
	members = append(members, start)
	members = append(members, path...)
	hopProofs := make([]importCycleHop, 0, len(proofs)+1)
	hopProofs = append(hopProofs, proofs...)
	hopProofs = append(hopProofs, closing)
	steps := make([]importCycleStep, 0, len(members))
	for index, member := range members {
		node := meta[member]
		proof := hopProofs[index]
		steps = append(steps, importCycleStep{
			repoID:       node.repoID,
			repoName:     node.repoName,
			file:         node.file,
			sourceModule: node.sourceModule,
			targetModule: proof.targetModule,
			lineNumber:   proof.lineNumber,
		})
	}
	return importCycle{steps: steps}
}

// importCycleNodeKey orders raw file nodes by repository, then file path.
func importCycleNodeKey(repoID, file string) string {
	return repoID + "\x00" + file
}

// importCycleKey is the rotation-dedupe identity of one enumerated cycle:
// its repository plus its normalized file order.
func importCycleKey(cycle importCycle) string {
	if len(cycle.steps) == 0 {
		return ""
	}
	files := make([]string, 0, len(cycle.steps))
	for _, step := range cycle.steps {
		files = append(files, step.file)
	}
	return cycle.steps[0].repoID + "\x00" + strings.Join(files, "\x00")
}

// earlierImportCycleHop prefers the earliest positive source line for one
// collapsed pair, breaking ties on the imported module name so duplicate
// edges resolve deterministically.
func earlierImportCycleHop(candidate, current importCycleHop) bool {
	if candidate.lineNumber <= 0 {
		return false
	}
	if current.lineNumber <= 0 {
		return true
	}
	if candidate.lineNumber != current.lineNumber {
		return candidate.lineNumber < current.lineNumber
	}
	return candidate.targetModule < current.targetModule
}

// importCycleRow shapes one enumerated simple cycle. The legacy scalar
// keys keep their reciprocal-era meaning for two-node cycles (the
// smallest file leads, the second file follows) and generalize to the
// first hop for longer ones; the full normalized order travels in the
// cycle_* internal keys the response shaper turns into cycle_length,
// cycle_path, and cycle_edges before stripping them.
func importCycleRow(cycle importCycle) map[string]any {
	first := cycle.steps[0]
	last := cycle.steps[len(cycle.steps)-1]
	files := make([]string, 0, len(cycle.steps))
	sourceModules := make([]string, 0, len(cycle.steps))
	targetModules := make([]string, 0, len(cycle.steps))
	lines := make([]int, 0, len(cycle.steps))
	for _, step := range cycle.steps {
		files = append(files, step.file)
		sourceModules = append(sourceModules, step.sourceModule)
		targetModules = append(targetModules, step.targetModule)
		lines = append(lines, step.lineNumber)
	}
	return map[string]any{
		"repo_id":               first.repoID,
		"repo_name":             first.repoName,
		"source_file":           first.file,
		"target_file":           cycle.steps[1].file,
		"source_module":         first.sourceModule,
		"target_module":         first.targetModule,
		"source_line_number":    first.lineNumber,
		"back_edge_line_number": last.lineNumber,
		"cycle_files":           files,
		"cycle_source_modules":  sourceModules,
		"cycle_target_modules":  targetModules,
		"cycle_lines":           lines,
	}
}

// importCycleRowMatches keeps a cycle when every directional anchor is
// empty or names any member of the cycle. Membership (not position)
// matching is what lets a file anchor find the 3- and 5-node cycles it
// belongs to however the normalized order leads them.
func importCycleRowMatches(req ImportDependencyRequest, row map[string]any) bool {
	if !matchesExactRequestValue(req.RepoID, querycontract.StringVal(row, "repo_id")) {
		return false
	}
	files := importCycleStringList(row, "cycle_files")
	modules := append(
		append([]string{}, importCycleStringList(row, "cycle_source_modules")...),
		importCycleStringList(row, "cycle_target_modules")...,
	)
	if len(files) == 0 {
		files = []string{querycontract.StringVal(row, "source_file"), querycontract.StringVal(row, "target_file")}
	}
	if len(modules) == 0 {
		modules = []string{querycontract.StringVal(row, "source_module"), querycontract.StringVal(row, "target_module")}
	}
	return matchesAnyRequestValue(req.SourceFile, files) &&
		matchesAnyRequestValue(req.TargetFile, files) &&
		matchesAnyRequestValue(req.SourceModule, modules) &&
		matchesAnyRequestValue(req.TargetModule, modules)
}

// matchesAnyRequestValue keeps an empty anchor open and otherwise
// requires an exact member hit.
func matchesAnyRequestValue(requested string, members []string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}
	for _, member := range members {
		if member == requested {
			return true
		}
	}
	return false
}

// importCycleStringList reads one internal cycle list back as strings,
// tolerating the []any form JSON round-trips produce.
func importCycleStringList(row map[string]any, key string) []string {
	switch values := row[key].(type) {
	case []string:
		return append([]string{}, values...)
	case []any:
		members := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			members = append(members, text)
		}
		return members
	default:
		return nil
	}
}

func importCycleRowKey(row map[string]any) string {
	return strings.Join([]string{
		querycontract.StringVal(row, "repo_id"),
		strings.Join(importCycleStringList(row, "cycle_files"), "\x01"),
	}, "\x00")
}

// compareImportCycleRows orders cycle rows length-ascending, then
// normalized path (repository first, then files), matching the
// enumeration contract (#6851). Legacy scalar keys break any residual
// tie before the proof line numbers do.
func compareImportCycleRows(left, right map[string]any) int {
	leftFiles := importCycleStringList(left, "cycle_files")
	rightFiles := importCycleStringList(right, "cycle_files")
	if len(leftFiles) != len(rightFiles) {
		return compareInts(len(leftFiles), len(rightFiles))
	}
	comparison := compareRowStrings(left, right, []string{"repo_id"})
	if comparison != 0 {
		return comparison
	}
	if path := compareStrings(
		[]string{strings.Join(leftFiles, "\x00")},
		[]string{strings.Join(rightFiles, "\x00")},
	); path != 0 {
		return path
	}
	comparison = compareRowStrings(left, right, []string{
		"source_file", "target_file", "source_module", "target_module",
	})
	if comparison != 0 {
		return comparison
	}
	if leftLine, rightLine := querycontract.IntVal(left, "source_line_number"), querycontract.IntVal(right, "source_line_number"); leftLine != rightLine {
		return compareInts(leftLine, rightLine)
	}
	return compareInts(querycontract.IntVal(left, "back_edge_line_number"), querycontract.IntVal(right, "back_edge_line_number"))
}

// compareImportCycles orders cycles length-ascending, then normalized
// path, so responses are deterministic across runs.
func compareImportCycles(left, right importCycle) int {
	if len(left.steps) != len(right.steps) {
		return compareInts(len(left.steps), len(right.steps))
	}
	for index := range left.steps {
		if left.steps[index].file != right.steps[index].file {
			return compareStrings(
				[]string{left.steps[index].file},
				[]string{right.steps[index].file},
			)
		}
	}
	leftKey := importCycleKey(left)
	rightKey := importCycleKey(right)
	if leftKey != rightKey {
		return compareStrings([]string{leftKey}, []string{rightKey})
	}
	for index := range left.steps {
		if left.steps[index].lineNumber != right.steps[index].lineNumber {
			return compareInts(left.steps[index].lineNumber, right.steps[index].lineNumber)
		}
	}
	return 0
}
