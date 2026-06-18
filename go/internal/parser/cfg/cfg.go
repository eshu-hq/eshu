package cfg

import "sort"

// BlockID identifies a basic block within one function CFG. IDs are assigned in
// construction order starting at zero.
type BlockID int

// StmtID identifies a statement (program point) within one function CFG. IDs are
// assigned in construction order starting at zero and are unique across blocks.
type StmtID int

// invalidStmt marks a statement that could not be created (for example a def or
// use against an unknown block). Callers may ignore it.
const invalidStmt StmtID = -1

// Stmt is a single program point: the bindings it defines and the bindings it
// uses. A statement may both use and define the same binding (for example
// x = x + 1); uses observe the definitions reaching the statement entry, before
// the statement's own definitions take effect.
type Stmt struct {
	ID   int
	Line int
	Defs []string
	Uses []string
}

// Block is a basic block: a maximal straight-line run of statements with a
// single entry. Succs lists successor block IDs in ascending order.
type Block struct {
	ID    int
	Stmts []Stmt
	Succs []int
}

// DefUse is a resolved reaching definition: the definition at DefStmt reaches
// the use of Binding at UseStmt. DefStmt and UseStmt are statement IDs; the Line
// fields carry the 1-based source lines for operator-facing facts.
type DefUse struct {
	Binding string
	DefStmt int
	DefLine int
	UseStmt int
	UseLine int
}

// Overflow counts data dropped because a Limits cap tripped. All zero means the
// Function is complete.
type Overflow struct {
	Blocks      int
	Stmts       int
	DefUseEdges int
	AccessPaths int
}

// Any reports whether any cap tripped.
func (o Overflow) Any() bool {
	return o.Blocks > 0 || o.Stmts > 0 || o.DefUseEdges > 0 || o.AccessPaths > 0
}

// Function is the bounded, deterministic result of Build: the basic blocks and
// the def->use edges reaching definitions resolved across the CFG.
type Function struct {
	Blocks   []Block
	DefUses  []DefUse
	Overflow Overflow
}

type builderBlock struct {
	stmts []Stmt
	succs map[BlockID]struct{}
}

// Builder accumulates basic blocks, statements, and control-flow edges for one
// function, then resolves them into a Function via Build. A Builder is not safe
// for concurrent use; construct one per function.
type Builder struct {
	limits   Limits
	entry    BlockID
	hasEntry bool
	blocks   []*builderBlock
	nextStmt int
}

// NewBuilder returns a Builder bounded by limits; non-positive caps fall back to
// DefaultLimits values.
func NewBuilder(limits Limits) *Builder {
	return &Builder{limits: limits.normalized()}
}

// AddBlock appends a new empty basic block and returns its ID.
func (b *Builder) AddBlock() BlockID {
	id := BlockID(len(b.blocks))
	b.blocks = append(b.blocks, &builderBlock{succs: map[BlockID]struct{}{}})
	if !b.hasEntry {
		b.entry = id
		b.hasEntry = true
	}
	return id
}

// SetEntry marks block as the function entry. When unset, the first added block
// is the entry.
func (b *Builder) SetEntry(block BlockID) {
	if b.validBlock(block) {
		b.entry = block
		b.hasEntry = true
	}
}

// AddStmt appends a statement defining defs and using uses to block and returns
// its StmtID. Empty binding names are dropped. A statement against an unknown
// block is ignored and returns invalidStmt.
func (b *Builder) AddStmt(block BlockID, line int, defs, uses []string) StmtID {
	if !b.validBlock(block) {
		return invalidStmt
	}
	id := b.nextStmt
	b.nextStmt++
	stmt := Stmt{
		ID:   id,
		Line: line,
		Defs: cleanBindings(defs),
		Uses: cleanBindings(uses),
	}
	bb := b.blocks[block]
	bb.stmts = append(bb.stmts, stmt)
	return StmtID(id)
}

// AddEdge records a control-flow edge from one block to another. Self-edges and
// duplicate edges are de-duplicated; edges referencing unknown blocks are
// ignored.
func (b *Builder) AddEdge(from, to BlockID) {
	if !b.validBlock(from) || !b.validBlock(to) {
		return
	}
	b.blocks[from].succs[to] = struct{}{}
}

func (b *Builder) validBlock(block BlockID) bool {
	return block >= 0 && int(block) < len(b.blocks)
}

// cleanBindings copies non-empty, de-duplicated binding names preserving first
// occurrence order so statement facts stay deterministic without sorting away
// source order.
func cleanBindings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, name := range in {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sortedSuccs returns a block's successors as a sorted int slice.
func (bb *builderBlock) sortedSuccs() []int {
	if len(bb.succs) == 0 {
		return nil
	}
	out := make([]int, 0, len(bb.succs))
	for s := range bb.succs {
		out = append(out, int(s))
	}
	sort.Ints(out)
	return out
}
