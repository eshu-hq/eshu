// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taint

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
)

// graph is the def-level value-flow graph derived from a control-flow graph: one
// node per (statement, binding) definition, with edges along def->use->new-def
// chains. It is the substrate the taint fixpoint runs over.
type graph struct {
	defs          []defNode
	defIndex      map[StmtBinding]int
	succs         [][]defEdge
	reach         map[StmtBinding][]int // (useStmt, binding) -> reaching defIDs
	stmtUses      map[int][]string
	stmtLine      map[int]int
	stmtBlock     map[int]int
	guardsByBlock map[int][]cfg.ControlDependence
}

type defNode struct {
	stmt    int
	binding string
	line    int
}

// defEdge links a definition to a definition it flows into, recording the
// statement the flow passes through (which may be a sanitizer).
type defEdge struct {
	to      int
	viaStmt int
}

// taintState is the per-definition lattice value: whether the definition is
// tainted, the neutralized sink-kind set (intersected across paths), and the
// originating source for provenance.
type taintState struct {
	tainted     bool
	set         bool
	neutralized map[Kind]struct{}
	origin      SourceMark
	originStmt  int
	originLine  int
}

// newGraph indexes a control-flow graph's definitions and def->use edges into
// the value-flow graph the fixpoint walks.
func newGraph(fn cfg.Function) *graph {
	g := &graph{
		defIndex:      map[StmtBinding]int{},
		reach:         map[StmtBinding][]int{},
		stmtUses:      map[int][]string{},
		stmtLine:      map[int]int{},
		stmtBlock:     map[int]int{},
		guardsByBlock: map[int][]cfg.ControlDependence{},
	}
	stmtDefs := map[int][]string{}
	for _, block := range fn.Blocks {
		for _, stmt := range block.Stmts {
			g.stmtLine[stmt.ID] = stmt.Line
			g.stmtBlock[stmt.ID] = block.ID
			if len(stmt.Uses) > 0 {
				g.stmtUses[stmt.ID] = stmt.Uses
			}
			if len(stmt.Defs) > 0 {
				stmtDefs[stmt.ID] = stmt.Defs
			}
			for _, binding := range stmt.Defs {
				key := StmtBinding{Stmt: stmt.ID, Binding: binding}
				if _, ok := g.defIndex[key]; ok {
					continue
				}
				g.defIndex[key] = len(g.defs)
				g.defs = append(g.defs, defNode{stmt: stmt.ID, binding: binding, line: stmt.Line})
			}
		}
	}
	for _, dep := range fn.ControlDependencies {
		g.guardsByBlock[dep.DependentBlock] = append(g.guardsByBlock[dep.DependentBlock], dep)
	}

	g.succs = make([][]defEdge, len(g.defs))
	for _, du := range fn.DefUses {
		defID, ok := g.defIndex[StmtBinding{Stmt: du.DefStmt, Binding: du.Binding}]
		if !ok {
			continue
		}
		useKey := StmtBinding{Stmt: du.UseStmt, Binding: du.Binding}
		g.reach[useKey] = append(g.reach[useKey], defID)
		for _, produced := range stmtDefs[du.UseStmt] {
			toID, ok := g.defIndex[StmtBinding{Stmt: du.UseStmt, Binding: produced}]
			if !ok {
				continue
			}
			g.succs[defID] = append(g.succs[defID], defEdge{to: toID, viaStmt: du.UseStmt})
		}
	}
	return g
}

// propagate runs the monotone taint fixpoint and returns the per-definition
// state. Taint can only turn on (monotone up) and a neutralized set can only
// shrink under intersection (monotone down), so the worklist terminates.
func (g *graph) propagate(facts Facts) []taintState {
	states := make([]taintState, len(g.defs))
	var work []int
	queued := make([]bool, len(g.defs))

	// Seed sources in a deterministic order (sorted by definition) so that, if a
	// definition is ever named by more than one source mark, the chosen origin is
	// stable across runs rather than depending on map iteration order.
	for _, seed := range sortedSources(facts.Sources) {
		id, ok := g.defIndex[seed.binding]
		if !ok {
			continue
		}
		states[id] = taintState{
			tainted:     true,
			set:         true,
			neutralized: map[Kind]struct{}{},
			origin:      seed.mark,
			originStmt:  seed.binding.Stmt,
			originLine:  g.stmtLine[seed.binding.Stmt],
		}
		if !queued[id] {
			work = append(work, id)
			queued[id] = true
		}
	}
	sort.Ints(work)

	for len(work) > 0 {
		from := work[0]
		work = work[1:]
		queued[from] = false

		for _, edge := range g.succs[from] {
			incoming := cloneKinds(states[from].neutralized)
			if san, ok := facts.Sanitizers[edge.viaStmt]; ok {
				for _, k := range san.Neutralizes {
					incoming[k] = struct{}{}
				}
			}
			if g.mergeState(&states[edge.to], incoming, states[from]) && !queued[edge.to] {
				work = append(work, edge.to)
				queued[edge.to] = true
			}
		}
	}
	return states
}

// mergeState folds an incoming tainted path into a destination definition's
// state. The first tainted path establishes the state; later paths intersect the
// neutralized set so a kind survives only when every path neutralized it. Origin
// is kept from the first path for stable provenance. A definition that is itself
// a source keeps an empty neutralized set (it cannot be sanitized at its
// origin).
func (g *graph) mergeState(dst *taintState, incoming map[Kind]struct{}, from taintState) bool {
	if !dst.set {
		dst.set = true
		dst.tainted = true
		dst.neutralized = incoming
		dst.origin = from.origin
		dst.originStmt = from.originStmt
		dst.originLine = from.originLine
		return true
	}
	if len(dst.neutralized) == 0 {
		return false // already minimal; intersection cannot shrink further
	}
	before := len(dst.neutralized)
	for k := range dst.neutralized {
		if _, ok := incoming[k]; !ok {
			delete(dst.neutralized, k)
		}
	}
	return len(dst.neutralized) != before
}

// evaluateSinks reports a finding for every tainted definition reaching a sink
// use. A sink kind in the definition's neutralized set yields a SANITIZES
// finding; otherwise a TAINTED finding. Emission stops at maxFindings in
// deterministic order and the second return value counts dropped findings.
func (g *graph) evaluateSinks(facts Facts, states []taintState, maxFindings int) ([]Finding, int) {
	sinkStmts := make([]int, 0, len(facts.Sinks))
	for stmt := range facts.Sinks {
		sinkStmts = append(sinkStmts, stmt)
	}
	sort.Ints(sinkStmts)

	var findings []Finding
	generated := 0
	for _, sinkStmt := range sinkStmts {
		mark := facts.Sinks[sinkStmt]
		for _, binding := range g.stmtUses[sinkStmt] {
			reaching := append([]int(nil), g.reach[StmtBinding{Stmt: sinkStmt, Binding: binding}]...)
			sort.Ints(reaching)
			for _, defID := range reaching {
				st := states[defID]
				if !st.tainted {
					continue
				}
				kind := FindingTainted
				confidence := taintedConfidence
				if _, neutralized := st.neutralized[mark.Kind]; neutralized {
					kind = FindingSanitized
					confidence = sanitizedConfidence
				}
				generated++
				if len(findings) >= maxFindings {
					continue
				}
				findings = append(findings, Finding{
					Kind:        kind,
					SinkKind:    mark.Kind,
					SinkLabel:   mark.Label,
					SourceKind:  st.origin.Kind,
					SourceLabel: st.origin.Label,
					Binding:     binding,
					SourceStmt:  st.originStmt,
					SourceLine:  st.originLine,
					SinkStmt:    sinkStmt,
					SinkLine:    g.stmtLine[sinkStmt],
					GuardReason: g.guardReasonForStmt(sinkStmt),
					Neutralized: sortedKinds(st.neutralized),
					Confidence:  confidence,
				})
			}
		}
	}
	return findings, generated - len(findings)
}

func (g *graph) guardReasonForStmt(stmt int) string {
	block, ok := g.stmtBlock[stmt]
	if !ok {
		return ""
	}
	deps := g.guardsByBlock[block]
	if len(deps) == 0 {
		return ""
	}
	reasons := g.guardReasonsForBlock(block, map[int]struct{}{})
	return strings.Join(reasons, " && ")
}

func (g *graph) guardReasonsForBlock(block int, seen map[int]struct{}) []string {
	if _, ok := seen[block]; ok {
		return nil
	}
	seen[block] = struct{}{}
	var reasons []string
	for _, dep := range g.guardsByBlock[block] {
		reasons = append(reasons, g.guardReasonsForBlock(dep.GuardBlock, seen)...)
		if dep.Guard != "" {
			reasons = append(reasons, dep.Guard)
		}
	}
	return reasons
}

// sourceSeed pairs a source definition with its mark for deterministic seeding.
type sourceSeed struct {
	binding StmtBinding
	mark    SourceMark
}

// sortedSources returns the source facts ordered by (statement, binding) so the
// seed loop does not depend on map iteration order.
func sortedSources(sources map[StmtBinding]SourceMark) []sourceSeed {
	out := make([]sourceSeed, 0, len(sources))
	for sb, mark := range sources {
		out = append(out, sourceSeed{binding: sb, mark: mark})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].binding.Stmt != out[j].binding.Stmt {
			return out[i].binding.Stmt < out[j].binding.Stmt
		}
		return out[i].binding.Binding < out[j].binding.Binding
	})
	return out
}

// cloneKinds returns a copy of a neutralized set.
func cloneKinds(in map[Kind]struct{}) map[Kind]struct{} {
	out := make(map[Kind]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

// sortedKinds returns the kinds of a set as a sorted slice for stable output.
func sortedKinds(in map[Kind]struct{}) []Kind {
	if len(in) == 0 {
		return nil
	}
	out := make([]Kind, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
