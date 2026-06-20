package taint

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
)

// findingsByKind counts findings of each FindingKind.
func findingsByKind(res Result) (tainted, sanitized int) {
	for _, f := range res.Findings {
		switch f.Kind {
		case FindingTainted:
			tainted++
		case FindingSanitized:
			sanitized++
		}
	}
	return tainted, sanitized
}

// TestSourceReachesSinkNoSanitizer proves an unsanitized source flowing to a
// sink reports a TAINTED finding of the sink's kind.
func TestSourceReachesSinkNoSanitizer(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"x"}, nil))
	s1 := int(b.AddStmt(blk, 2, nil, []string{"x"}))
	fn := b.Build()

	facts := Facts{
		Sources: map[StmtBinding]SourceMark{{Stmt: s0, Binding: "x"}: {Kind: "http", Label: "req"}},
		Sinks:   map[int]SinkMark{s1: {Kind: "sql", Label: "db.Query"}},
	}
	res := Analyze(fn, facts, DefaultLimits())

	tainted, sanitized := findingsByKind(res)
	if tainted != 1 || sanitized != 0 {
		t.Fatalf("got tainted=%d sanitized=%d, want 1/0; findings=%+v", tainted, sanitized, res.Findings)
	}
	if got := res.Findings[0].SinkKind; got != "sql" {
		t.Fatalf("sink kind = %q, want sql", got)
	}
	if got := res.Findings[0].SourceKind; got != "http" {
		t.Fatalf("source kind = %q, want http", got)
	}
}

// TestCorrectSanitizerSuppresses proves a sanitizer that neutralizes the sink's
// kind suppresses the TAINTED finding and reports SANITIZES instead.
func TestCorrectSanitizerSuppresses(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"x"}, nil))
	s1 := int(b.AddStmt(blk, 2, []string{"y"}, []string{"x"})) // y = sqlEscape(x)
	s2 := int(b.AddStmt(blk, 3, nil, []string{"y"}))           // db.Query(y)
	fn := b.Build()

	facts := Facts{
		Sources:    map[StmtBinding]SourceMark{{Stmt: s0, Binding: "x"}: {Kind: "http"}},
		Sanitizers: map[int]SanitizerMark{s1: {Neutralizes: []Kind{"sql"}}},
		Sinks:      map[int]SinkMark{s2: {Kind: "sql"}},
	}
	res := Analyze(fn, facts, DefaultLimits())

	tainted, sanitized := findingsByKind(res)
	if tainted != 0 || sanitized != 1 {
		t.Fatalf("got tainted=%d sanitized=%d, want 0/1; findings=%+v", tainted, sanitized, res.Findings)
	}
}

// TestWrongKindSanitizerDoesNotSuppress proves a sanitizer that neutralizes a
// different kind than the sink does NOT suppress the flow (the kind-set model).
func TestWrongKindSanitizerDoesNotSuppress(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"x"}, nil))
	s1 := int(b.AddStmt(blk, 2, []string{"y"}, []string{"x"})) // y = htmlEscape(x)
	s2 := int(b.AddStmt(blk, 3, nil, []string{"y"}))           // db.Query(y)
	fn := b.Build()

	facts := Facts{
		Sources:    map[StmtBinding]SourceMark{{Stmt: s0, Binding: "x"}: {Kind: "http"}},
		Sanitizers: map[int]SanitizerMark{s1: {Neutralizes: []Kind{"html"}}},
		Sinks:      map[int]SinkMark{s2: {Kind: "sql"}},
	}
	res := Analyze(fn, facts, DefaultLimits())

	tainted, _ := findingsByKind(res)
	if tainted != 1 {
		t.Fatalf("got tainted=%d, want 1 (html sanitizer must not suppress a sql sink); findings=%+v", tainted, res.Findings)
	}
}

// TestGuardedSinkReportsGuardReason proves a tainted sink inside a
// control-dependent branch carries the gating predicate as finding provenance.
func TestGuardedSinkReportsGuardReason(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	entry := b.AddBlock()
	thenBlock := b.AddBlock()
	merge := b.AddBlock()
	s0 := int(b.AddStmt(entry, 1, []string{"q"}, nil))
	b.AddGuardStmt(entry, 2, []string{"allowed"}, "allowed")
	sSink := int(b.AddStmt(thenBlock, 3, nil, []string{"q"}))
	b.AddEdge(entry, thenBlock)
	b.AddEdge(entry, merge)
	b.AddEdge(thenBlock, merge)
	fn := b.Build()

	facts := Facts{
		Sources: map[StmtBinding]SourceMark{{Stmt: s0, Binding: "q"}: {Kind: "http"}},
		Sinks:   map[int]SinkMark{sSink: {Kind: "sql"}},
	}
	res := Analyze(fn, facts, DefaultLimits())
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want one guarded taint finding: %+v", len(res.Findings), res.Findings)
	}
	if got, want := res.Findings[0].GuardReason, "allowed"; got != want {
		t.Fatalf("GuardReason = %q, want %q; finding=%+v", got, want, res.Findings[0])
	}
}

// TestNestedGuardedSinkReportsStableGuardReason proves multiple controlling
// predicates are reported in deterministic outer-to-inner order.
func TestNestedGuardedSinkReportsStableGuardReason(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	entry := b.AddBlock()
	outerThen := b.AddBlock()
	innerThen := b.AddBlock()
	innerMerge := b.AddBlock()
	outerMerge := b.AddBlock()
	s0 := int(b.AddStmt(entry, 1, []string{"q"}, nil))
	b.AddGuardStmt(entry, 2, []string{"allowed"}, "allowed")
	b.AddGuardStmt(outerThen, 3, []string{"ready"}, "ready")
	sSink := int(b.AddStmt(innerThen, 4, nil, []string{"q"}))
	b.AddEdge(entry, outerThen)
	b.AddEdge(entry, outerMerge)
	b.AddEdge(outerThen, innerThen)
	b.AddEdge(outerThen, innerMerge)
	b.AddEdge(innerThen, innerMerge)
	b.AddEdge(innerMerge, outerMerge)
	fn := b.Build()

	facts := Facts{
		Sources: map[StmtBinding]SourceMark{{Stmt: s0, Binding: "q"}: {Kind: "http"}},
		Sinks:   map[int]SinkMark{sSink: {Kind: "sql"}},
	}
	res := Analyze(fn, facts, DefaultLimits())
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want one guarded taint finding: %+v", len(res.Findings), res.Findings)
	}
	if got, want := res.Findings[0].GuardReason, "allowed && ready"; got != want {
		t.Fatalf("GuardReason = %q, want %q; finding=%+v", got, want, res.Findings[0])
	}
}

// TestNeutralizedIntersectionAtMerge proves the neutralized set is intersected
// when two tainted values merge into one definition: a value sanitized for sql
// on one path and not on the other is NOT considered sql-safe at the merge.
func TestNeutralizedIntersectionAtMerge(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"x"}, nil))
	sA := int(b.AddStmt(blk, 2, []string{"a"}, []string{"x"}))       // a = sqlEscape(x)
	sB := int(b.AddStmt(blk, 3, []string{"bb"}, []string{"x"}))      // bb = x (no sanitize)
	sY := int(b.AddStmt(blk, 4, []string{"y"}, []string{"a", "bb"})) // y = a + bb
	sSink := int(b.AddStmt(blk, 5, nil, []string{"y"}))
	fn := b.Build()

	facts := Facts{
		Sources:    map[StmtBinding]SourceMark{{Stmt: s0, Binding: "x"}: {Kind: "http"}},
		Sanitizers: map[int]SanitizerMark{sA: {Neutralizes: []Kind{"sql"}}},
		Sinks:      map[int]SinkMark{sSink: {Kind: "sql"}},
	}
	_ = sB
	_ = sY
	res := Analyze(fn, facts, DefaultLimits())

	tainted, _ := findingsByKind(res)
	if tainted < 1 {
		t.Fatalf("got tainted=%d, want >=1 (unsanitized path must survive the merge); findings=%+v", tainted, res.Findings)
	}
}

// TestAnalyzeDeterministic proves Analyze yields identical findings across runs
// despite map-keyed facts, so the emitted bucket is byte-stable.
func TestAnalyzeDeterministic(t *testing.T) {
	t.Parallel()

	build := func() Result {
		b := cfg.NewBuilder(cfg.DefaultLimits())
		blk := b.AddBlock()
		s0 := int(b.AddStmt(blk, 1, []string{"x"}, nil))
		s1 := int(b.AddStmt(blk, 2, nil, []string{"x"}))
		s2 := int(b.AddStmt(blk, 3, nil, []string{"x"}))
		fn := b.Build()
		facts := Facts{
			Sources: map[StmtBinding]SourceMark{{Stmt: s0, Binding: "x"}: {Kind: "http"}},
			Sinks:   map[int]SinkMark{s1: {Kind: "sql"}, s2: {Kind: "command"}},
		}
		return Analyze(fn, facts, DefaultLimits())
	}

	first := build()
	for i := 0; i < 8; i++ {
		if got := build(); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs:\n got %+v\nwant %+v", i, got, first)
		}
	}
}

// TestFindingOverflowCounted proves finding emission stops at the cap and counts
// the dropped findings.
func TestFindingOverflowCounted(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"x"}, nil))
	s1 := int(b.AddStmt(blk, 2, nil, []string{"x"}))
	s2 := int(b.AddStmt(blk, 3, nil, []string{"x"}))
	s3 := int(b.AddStmt(blk, 4, nil, []string{"x"}))
	fn := b.Build()

	facts := Facts{
		Sources: map[StmtBinding]SourceMark{{Stmt: s0, Binding: "x"}: {Kind: "http"}},
		Sinks: map[int]SinkMark{
			s1: {Kind: "sql"},
			s2: {Kind: "sql"},
			s3: {Kind: "sql"},
		},
	}
	res := Analyze(fn, facts, Limits{MaxFindings: 1})
	if len(res.Findings) != 1 {
		t.Fatalf("emitted %d findings, want 1 (cap)", len(res.Findings))
	}
	if res.Overflow != 2 {
		t.Fatalf("overflow = %d, want 2", res.Overflow)
	}
}
