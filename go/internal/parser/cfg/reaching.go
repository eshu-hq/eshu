package cfg

import "sort"

// defUseLess orders def->use edges deterministically by use point, then
// definition point, then binding name.
func defUseLess(a, b DefUse) bool {
	if a.UseStmt != b.UseStmt {
		return a.UseStmt < b.UseStmt
	}
	if a.DefStmt != b.DefStmt {
		return a.DefStmt < b.DefStmt
	}
	return a.Binding < b.Binding
}

// defSite is one definition of a binding at a program point, identified by a
// dense defID used as the element of reaching-definition sets.
type defSite struct {
	binding string
	stmt    int
	line    int
}

// defSet is a set of dense defIDs. Reaching-definition lattice values are
// defSets; the lattice is finite (bounded by the number of definitions) and the
// transfer function is monotone, so the worklist reaches a fixpoint.
type defSet map[int]struct{}

func (s defSet) clone() defSet {
	out := make(defSet, len(s))
	for id := range s {
		out[id] = struct{}{}
	}
	return out
}

func (s defSet) equal(other defSet) bool {
	if len(s) != len(other) {
		return false
	}
	for id := range s {
		if _, ok := other[id]; !ok {
			return false
		}
	}
	return true
}

// Build resolves the accumulated blocks, statements, and edges into a Function:
// it emits the basic-block structure and runs the reaching-definitions fixpoint
// to produce def->use edges. Build is pure with respect to construction order:
// the same Builder calls always yield the same Function.
func (b *Builder) Build() Function {
	blocks := b.emitBlocks()
	fn := Function{Blocks: blocks}
	if len(blocks) == 0 {
		return fn
	}

	totalStmts := 0
	for i := range blocks {
		totalStmts += len(blocks[i].Stmts)
	}
	// Guard the fixpoint cost: past a cap, emit the CFG structure but skip
	// reaching definitions and record the count that tripped the cap.
	if len(blocks) > b.limits.MaxBlocks {
		fn.Overflow.Blocks = len(blocks)
		return fn
	}
	if totalStmts > b.limits.MaxStmts {
		fn.Overflow.Stmts = totalStmts
		return fn
	}

	defs, defsByBinding, stmtDefIDs := indexDefs(blocks)
	in := b.solveReaching(blocks, defs, defsByBinding, stmtDefIDs)
	fn.DefUses, fn.Overflow.DefUseEdges = emitDefUses(
		blocks, defs, defsByBinding, stmtDefIDs, in, b.limits.MaxDefUseEdges)
	fn.ControlDependencies, fn.Overflow.ControlDependencies = computeControlDependencies(
		blocks, b.limits.MaxControlDependencies)
	return fn
}

// indexDefs assigns a dense defID to every (statement, defined-binding) pair in
// construction order and returns the def table, a binding->defIDs index used for
// kills, and a statement->defIDs index used to apply a statement's own defs.
func indexDefs(blocks []Block) (defs []defSite, defsByBinding map[string][]int, stmtDefIDs map[int][]int) {
	defsByBinding = map[string][]int{}
	stmtDefIDs = map[int][]int{}
	for i := range blocks {
		for _, stmt := range blocks[i].Stmts {
			for _, binding := range stmt.Defs {
				id := len(defs)
				defs = append(defs, defSite{binding: binding, stmt: stmt.ID, line: stmt.Line})
				defsByBinding[binding] = append(defsByBinding[binding], id)
				stmtDefIDs[stmt.ID] = append(stmtDefIDs[stmt.ID], id)
			}
		}
	}
	return defs, defsByBinding, stmtDefIDs
}

// transfer applies a block's statements to an entry reaching-set and returns the
// exit reaching-set. A statement kills every prior definition of each binding it
// defines, then adds its own definitions.
func transfer(block Block, in defSet, defs []defSite, defsByBinding map[string][]int, stmtDefIDs map[int][]int) defSet {
	cur := in.clone()
	for _, stmt := range block.Stmts {
		for _, binding := range stmt.Defs {
			for _, killID := range defsByBinding[binding] {
				delete(cur, killID)
			}
		}
		for _, id := range stmtDefIDs[stmt.ID] {
			cur[id] = struct{}{}
		}
	}
	return cur
}

// solveReaching runs the monotone reaching-definitions fixpoint with a worklist
// and returns the entry reaching-set for each block, indexed by block ID. Only
// blocks whose predecessor out-sets changed are recomputed, so a stable region
// of the CFG is not re-merged every round. The function entry receives an empty
// in-set because parameters are modeled as definitions inside the entry block.
// Processing order does not affect the result (the analysis is monotone), and
// emitted output is sorted independently, so determinism is preserved.
func (b *Builder) solveReaching(blocks []Block, defs []defSite, defsByBinding map[string][]int, stmtDefIDs map[int][]int) []defSet {
	preds := predecessors(blocks)
	in := make([]defSet, len(blocks))
	out := make([]defSet, len(blocks))
	for i := range blocks {
		in[i] = defSet{}
		out[i] = defSet{}
	}

	// Seed the worklist with every block in ascending ID order; queued tracks
	// membership so a block is never enqueued twice.
	work := make([]int, len(blocks))
	queued := make([]bool, len(blocks))
	for i := range blocks {
		work[i] = i
		queued[i] = true
	}

	for len(work) > 0 {
		id := work[0]
		work = work[1:]
		queued[id] = false

		merged := defSet{}
		if BlockID(id) != b.entry {
			for _, pred := range preds[id] {
				for defID := range out[pred] {
					merged[defID] = struct{}{}
				}
			}
		}
		in[id] = merged

		next := transfer(blocks[id], merged, defs, defsByBinding, stmtDefIDs)
		if next.equal(out[id]) {
			continue
		}
		out[id] = next
		// A changed out-set can only affect successors; re-enqueue them.
		for _, succ := range blocks[id].Succs {
			if succ >= 0 && succ < len(blocks) && !queued[succ] {
				work = append(work, succ)
				queued[succ] = true
			}
		}
	}
	return in
}

// predecessors inverts the successor edges into a per-block predecessor list.
func predecessors(blocks []Block) [][]int {
	preds := make([][]int, len(blocks))
	for id := range blocks {
		for _, succ := range blocks[id].Succs {
			if succ >= 0 && succ < len(blocks) {
				preds[succ] = append(preds[succ], id)
			}
		}
	}
	return preds
}

// emitDefUses replays each block from its entry reaching-set, recording a
// def->use edge for every use against each definition that reaches it. Emission
// stops at maxEdges in deterministic replay order; the second return value
// counts edges dropped past the cap.
func emitDefUses(blocks []Block, defs []defSite, defsByBinding map[string][]int, stmtDefIDs map[int][]int, in []defSet, maxEdges int) ([]DefUse, int) {
	var out []DefUse
	generated := 0
	for id := range blocks {
		cur := in[id].clone()
		for _, stmt := range blocks[id].Stmts {
			for _, use := range stmt.Uses {
				for _, defID := range defsByBinding[use] {
					if _, reaches := cur[defID]; !reaches {
						continue
					}
					generated++
					if len(out) >= maxEdges {
						continue
					}
					out = append(out, DefUse{
						Binding: use,
						DefStmt: defs[defID].stmt,
						DefLine: defs[defID].line,
						UseStmt: stmt.ID,
						UseLine: stmt.Line,
					})
				}
			}
			// Apply this statement's definitions before the next statement.
			for _, binding := range stmt.Defs {
				for _, killID := range defsByBinding[binding] {
					delete(cur, killID)
				}
			}
			for _, defID := range stmtDefIDs[stmt.ID] {
				cur[defID] = struct{}{}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return defUseLess(out[i], out[j]) })
	return out, generated - len(out)
}

// emitBlocks renders the basic-block facts in construction order with sorted
// successors.
func (b *Builder) emitBlocks() []Block {
	if len(b.blocks) == 0 {
		return nil
	}
	out := make([]Block, 0, len(b.blocks))
	for id, bb := range b.blocks {
		out = append(out, Block{
			ID:         id,
			Stmts:      bb.stmts,
			Succs:      bb.sortedSuccs(),
			SuccGuards: bb.sortedSuccGuards(),
		})
	}
	return out
}
