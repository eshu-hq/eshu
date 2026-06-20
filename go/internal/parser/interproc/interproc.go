package interproc

import "sort"

// FunctionID is a durable, generation-independent function identity. It encodes
// the repository, so an edge between two FunctionIDs with different repositories
// is a cross-repo flow.
type FunctionID string

// SlotKind distinguishes the kinds of value position a port can name.
type SlotKind int

const (
	// SlotParam is a function parameter, identified by Index.
	SlotParam SlotKind = iota
	// SlotReturn is a function's return value.
	SlotReturn
	// SlotNamed is a named pseudo-slot: a captured closure variable or an object
	// field, identified by Name.
	SlotNamed
)

// Slot is a value position within a function.
type Slot struct {
	Kind  SlotKind
	Index int
	Name  string
}

// Port is a value position in a specific function: the node type of the graph.
type Port struct {
	Func FunctionID
	Slot Slot
}

// Edge is a directed value flow from one port to another.
type Edge struct {
	From Port
	To   Port
}

// Source marks a port where taint enters.
type Source struct {
	Port  Port
	Kind  string
	Label string
}

// Sink marks a port that is a vulnerability if tainted. Cloud marks a sink that
// is a correlated cloud fact, terminating a code-to-cloud path.
type Sink struct {
	Port  Port
	Kind  string
	Label string
	Cloud bool
}

// Sanitizer marks a port whose value is neutralized for the given sink kinds
// from that port onward.
type Sanitizer struct {
	Port        Port
	Neutralizes []string
}

// Program is the interprocedural value-flow graph.
type Program struct {
	Edges      []Edge
	Sources    []Source
	Sinks      []Sink
	Sanitizers []Sanitizer
}

// Limits bounds finding emission.
type Limits struct {
	MaxFindings int
}

// DefaultLimits returns the cap used when a caller supplies none.
func DefaultLimits() Limits {
	return Limits{MaxFindings: 8192}
}

func (l Limits) normalized() Limits {
	if l.MaxFindings <= 0 {
		l.MaxFindings = DefaultLimits().MaxFindings
	}
	return l
}

// interprocConfidence is the fixed confidence for interprocedural findings:
// lower than intraprocedural because it composes summaries across calls.
const interprocConfidence = 0.6

// Finding is one interprocedural source-to-sink taint path.
type Finding struct {
	SourceFunc     FunctionID
	SourceKind     string
	SourceLabel    string
	SinkFunc       FunctionID
	SinkKind       string
	SinkLabel      string
	SinkPort       Port
	Cloud          bool
	Neutralized    []string
	Confidence     float64
	Trail          []Port
	TrailTruncated bool
}

// Result is the bounded, deterministic output of a solve.
type Result struct {
	Findings []Finding
	Overflow int
}

// sortFindings orders findings deterministically.
func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		return findingLess(findings[i], findings[j])
	})
}

// findingLess is a total order over the identity-relevant fields of a Finding,
// so sorting (and therefore the cap boundary) is identical between Solve and
// SolvePartitioned regardless of pre-sort insertion order.
func findingLess(a, b Finding) bool {
	if a.SinkFunc != b.SinkFunc {
		return a.SinkFunc < b.SinkFunc
	}
	if a.SinkPort.Slot.Kind != b.SinkPort.Slot.Kind {
		return a.SinkPort.Slot.Kind < b.SinkPort.Slot.Kind
	}
	if a.SinkPort.Slot.Index != b.SinkPort.Slot.Index {
		return a.SinkPort.Slot.Index < b.SinkPort.Slot.Index
	}
	if a.SinkPort.Slot.Name != b.SinkPort.Slot.Name {
		return a.SinkPort.Slot.Name < b.SinkPort.Slot.Name
	}
	if a.SinkKind != b.SinkKind {
		return a.SinkKind < b.SinkKind
	}
	if a.SourceFunc != b.SourceFunc {
		return a.SourceFunc < b.SourceFunc
	}
	if a.SourceKind != b.SourceKind {
		return a.SourceKind < b.SourceKind
	}
	if a.SourceLabel != b.SourceLabel {
		return a.SourceLabel < b.SourceLabel
	}
	return a.SinkLabel < b.SinkLabel
}
