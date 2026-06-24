// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package interproc

import (
	"fmt"
	"reflect"
	"testing"
)

func param(fn FunctionID, i int) Port { return Port{Func: fn, Slot: Slot{Kind: SlotParam, Index: i}} }
func ret(fn FunctionID) Port          { return Port{Func: fn, Slot: Slot{Kind: SlotReturn}} }
func named(fn FunctionID, n string) Port {
	return Port{Func: fn, Slot: Slot{Kind: SlotNamed, Name: n}}
}

func taintedSinks(res Result) int {
	return len(res.Findings)
}

// TestCrossRepoSourceToSink proves a source in one repository reaching a sink in
// another via a cross-repo edge is reported.
func TestCrossRepoSourceToSink(t *testing.T) {
	t.Parallel()

	src := param("repoA\x1fhandler", 0)
	sink := param("repoB\x1flib", 0)
	program := Program{
		Edges:   []Edge{{From: src, To: sink}},
		Sources: []Source{{Port: src, Kind: "http", Label: "req"}},
		Sinks:   []Sink{{Port: sink, Kind: "sql", Label: "db.Query"}},
	}
	res := Solve(program, DefaultLimits())
	if taintedSinks(res) != 1 {
		t.Fatalf("findings = %d, want 1; %+v", taintedSinks(res), res.Findings)
	}
	f := res.Findings[0]
	if f.SourceFunc != "repoA\x1fhandler" || f.SinkFunc != "repoB\x1flib" || f.SinkKind != "sql" {
		t.Fatalf("unexpected finding %+v", f)
	}
}

// TestClosureCaptureFlow proves a value captured by a closure (a named slot)
// carries taint to a sink — a false-negative class summary-only engines miss.
func TestClosureCaptureFlow(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fouter", 0)
	captured := named("repo\x1fclosure", "captured")
	sinkPort := ret("repo\x1fclosure")
	program := Program{
		Edges: []Edge{
			{From: src, To: captured},      // closure captures the parameter
			{From: captured, To: sinkPort}, // captured value flows to the sink
		},
		Sources: []Source{{Port: src, Kind: "http"}},
		Sinks:   []Sink{{Port: sinkPort, Kind: "command", Label: "exec"}},
	}
	res := Solve(program, DefaultLimits())
	if taintedSinks(res) != 1 {
		t.Fatalf("closure flow not detected: %+v", res.Findings)
	}
}

// TestFieldFlow proves a value written to an object field (named slot) and read
// back flows to a sink — the other false-negative class.
func TestFieldFlow(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fwrite", 0)
	field := named("repo\x1fUser", "name")
	sinkPort := param("repo\x1frender", 0)
	program := Program{
		Edges: []Edge{
			{From: src, To: field},      // u.name = tainted
			{From: field, To: sinkPort}, // render(u.name)
		},
		Sources: []Source{{Port: src, Kind: "http"}},
		Sinks:   []Sink{{Port: sinkPort, Kind: "html"}},
	}
	res := Solve(program, DefaultLimits())
	if taintedSinks(res) != 1 {
		t.Fatalf("field flow not detected: %+v", res.Findings)
	}
}

// TestSanitizerKindSet proves the kind-set model across functions: a sanitizer
// for the sink's kind suppresses the finding, a wrong-kind one does not.
func TestSanitizerKindSet(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fsrc", 0)
	mid := named("repo\x1fmid", "out")
	sink := param("repo\x1fsink", 0)
	base := Program{
		Edges:   []Edge{{From: src, To: mid}, {From: mid, To: sink}},
		Sources: []Source{{Port: src, Kind: "http"}},
		Sinks:   []Sink{{Port: sink, Kind: "sql"}},
	}

	correct := base
	correct.Sanitizers = []Sanitizer{{Port: mid, Neutralizes: []string{"sql"}}}
	if got := taintedSinks(Solve(correct, DefaultLimits())); got != 0 {
		t.Fatalf("correct sanitizer did not suppress: %d findings", got)
	}

	wrong := base
	wrong.Sanitizers = []Sanitizer{{Port: mid, Neutralizes: []string{"html"}}}
	if got := taintedSinks(Solve(wrong, DefaultLimits())); got != 1 {
		t.Fatalf("wrong-kind sanitizer should not suppress: %d findings", got)
	}
}

// TestCloudSinkTerminates proves a sink that is a cloud fact terminates the path
// and is flagged, joining the code-to-cloud reachability terminal.
func TestCloudSinkTerminates(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fhandler", 0)
	sink := param("aws\x1fs3bucket", 0)
	program := Program{
		Edges:   []Edge{{From: src, To: sink}},
		Sources: []Source{{Port: src, Kind: "http"}},
		Sinks:   []Sink{{Port: sink, Kind: "cloud_write", Label: "s3:PutObject", Cloud: true}},
	}
	res := Solve(program, DefaultLimits())
	if taintedSinks(res) != 1 || !res.Findings[0].Cloud {
		t.Fatalf("cloud sink not terminated/flagged: %+v", res.Findings)
	}
}

// multiComponentProgram builds several independent source->sink components.
func multiComponentProgram(n int) Program {
	p := Program{}
	for i := 0; i < n; i++ {
		s := param(FunctionID("repo\x1fsrc"+string(rune('A'+i))), 0)
		k := param(FunctionID("repo\x1fsink"+string(rune('A'+i))), 0)
		p.Edges = append(p.Edges, Edge{From: s, To: k})
		p.Sources = append(p.Sources, Source{Port: s, Kind: "http"})
		p.Sinks = append(p.Sinks, Sink{Port: k, Kind: "sql"})
	}
	return p
}

// TestPartitionedEqualsSerial proves the partitioned, concurrent solver produces
// exactly the same findings as the serial solver. Run with -race to prove the
// concurrency is data-race free.
func TestPartitionedEqualsSerial(t *testing.T) {
	t.Parallel()

	program := multiComponentProgram(12)
	serial := Solve(program, DefaultLimits())
	partitioned := SolvePartitioned(program, DefaultLimits())
	if !reflect.DeepEqual(serial, partitioned) {
		t.Fatalf("partitioned != serial\n serial=%+v\n part=%+v", serial, partitioned)
	}
	if len(serial.Findings) != 12 {
		t.Fatalf("expected 12 findings, got %d", len(serial.Findings))
	}
}

// TestMultipleSourcesToOneSink proves both sources reaching one sink are
// reported (recall), each with its own origin — not collapsed to a single
// finding.
func TestMultipleSourcesToOneSink(t *testing.T) {
	t.Parallel()

	srcA := param("repo\x1fa", 0)
	srcB := param("repo\x1fb", 0)
	sink := param("repo\x1fsink", 0)
	program := Program{
		Edges:   []Edge{{From: srcA, To: sink}, {From: srcB, To: sink}},
		Sources: []Source{{Port: srcA, Kind: "http"}, {Port: srcB, Kind: "cli"}},
		Sinks:   []Sink{{Port: sink, Kind: "sql"}},
	}
	res := Solve(program, DefaultLimits())
	if len(res.Findings) != 2 {
		t.Fatalf("findings = %d, want 2 (one per source); %+v", len(res.Findings), res.Findings)
	}
	kinds := map[string]bool{}
	for _, f := range res.Findings {
		kinds[f.SourceKind] = true
	}
	if !kinds["http"] || !kinds["cli"] {
		t.Fatalf("missing a source kind; got %+v", res.Findings)
	}
}

// TestOriginAttributionUnsanitizedPath proves a sink reached by a sanitized path
// from one source and an unsanitized path from another fires, attributed to the
// unsanitized source only.
func TestOriginAttributionUnsanitizedPath(t *testing.T) {
	t.Parallel()

	srcSafe := param("repo\x1fsafe", 0)
	srcRaw := param("repo\x1fraw", 0)
	mid := named("repo\x1fmid", "out")
	sink := param("repo\x1fsink", 0)
	program := Program{
		Edges: []Edge{
			{From: srcSafe, To: mid}, // safe path goes through the sanitizer
			{From: mid, To: sink},
			{From: srcRaw, To: sink}, // raw path goes straight to the sink
		},
		Sources:    []Source{{Port: srcSafe, Kind: "safe"}, {Port: srcRaw, Kind: "raw"}},
		Sanitizers: []Sanitizer{{Port: mid, Neutralizes: []string{"sql"}}},
		Sinks:      []Sink{{Port: sink, Kind: "sql"}},
	}
	res := Solve(program, DefaultLimits())
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want 1 (only the raw path fires); %+v", len(res.Findings), res.Findings)
	}
	if res.Findings[0].SourceKind != "raw" {
		t.Fatalf("finding attributed to %q, want raw", res.Findings[0].SourceKind)
	}
}

// TestFindingTrailOrdersSourceIntermediateSink proves each finding carries the
// bounded source-to-sink port trail that explains why the value reached the sink.
func TestFindingTrailOrdersSourceIntermediateSink(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fsrc", 0)
	mid := named("repo\x1fmid", "value")
	sink := param("repo\x1fsink", 0)
	program := Program{
		Edges:   []Edge{{From: src, To: mid}, {From: mid, To: sink}},
		Sources: []Source{{Port: src, Kind: "http"}},
		Sinks:   []Sink{{Port: sink, Kind: "sql"}},
	}

	serial := Solve(program, DefaultLimits())
	partitioned := SolvePartitioned(program, DefaultLimits())
	if !reflect.DeepEqual(serial, partitioned) {
		t.Fatalf("partitioned != serial\n serial=%+v\n part=%+v", serial, partitioned)
	}
	if len(serial.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(serial.Findings), serial.Findings)
	}
	if got, want := serial.Findings[0].Trail, []Port{src, mid, sink}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trail = %+v, want %+v", got, want)
	}
	if serial.Findings[0].TrailTruncated {
		t.Fatalf("short trail unexpectedly marked truncated: %+v", serial.Findings[0])
	}
}

// TestFindingTrailKeepsSinkWhenTruncated proves long evidence trails keep the
// real terminal sink instead of presenting the last retained intermediate as
// the sink.
func TestFindingTrailKeepsSinkWhenTruncated(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fsrc", 0)
	sink := param("repo\x1fsink", 0)
	edges := make([]Edge, 0, maxFindingTrailPorts+8)
	previous := src
	for i := 0; i < maxFindingTrailPorts+8; i++ {
		next := named(FunctionID(fmt.Sprintf("repo\x1fmid%d", i)), "value")
		edges = append(edges, Edge{From: previous, To: next})
		previous = next
	}
	edges = append(edges, Edge{From: previous, To: sink})
	program := Program{
		Edges:   edges,
		Sources: []Source{{Port: src, Kind: "http"}},
		Sinks:   []Sink{{Port: sink, Kind: "sql"}},
	}

	res := Solve(program, DefaultLimits())
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(res.Findings), res.Findings)
	}
	finding := res.Findings[0]
	if !finding.TrailTruncated {
		t.Fatalf("long trail was not marked truncated: %+v", finding.Trail)
	}
	if got, want := len(finding.Trail), maxFindingTrailPorts; got != want {
		t.Fatalf("trail length = %d, want %d", got, want)
	}
	if got := finding.Trail[0]; got != src {
		t.Fatalf("trail source = %+v, want %+v", got, src)
	}
	if got := finding.Trail[len(finding.Trail)-1]; got != sink {
		t.Fatalf("trail terminal = %+v, want sink %+v", got, sink)
	}
}

// TestFindingTrailUpdatesWhenNeutralizationShrinks proves the reported trail
// follows the path that actually keeps the sink unsuppressed.
func TestFindingTrailUpdatesWhenNeutralizationShrinks(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fsrc", 0)
	mid := named("repo\x1fmid", "sanitized")
	sink := param("repo\x1fsink", 0)
	program := Program{
		Edges: []Edge{
			{From: src, To: mid},
			{From: mid, To: sink},
			{From: src, To: sink},
		},
		Sources:    []Source{{Port: src, Kind: "http"}},
		Sanitizers: []Sanitizer{{Port: mid, Neutralizes: []string{"sql"}}},
		Sinks:      []Sink{{Port: sink, Kind: "sql"}},
	}

	res := Solve(program, DefaultLimits())
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(res.Findings), res.Findings)
	}
	if got, want := res.Findings[0].Trail, []Port{src, sink}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trail = %+v, want unsanitized direct path %+v", got, want)
	}
}

// TestFindingTrailMatchesSinkKindWitness proves each sink kind receives a trail
// that actually avoids the sanitizer for that kind, even when another path only
// explains a different unsuppressed sink kind.
func TestFindingTrailMatchesSinkKindWitness(t *testing.T) {
	t.Parallel()

	src := param("repo\x1fsrc", 0)
	sqlOnly := named("repo\x1fmid", "sql-safe")
	xssOnly := named("repo\x1fmid", "xss-safe")
	sink := param("repo\x1fsink", 0)
	program := Program{
		Edges: []Edge{
			{From: src, To: sqlOnly},
			{From: sqlOnly, To: sink},
			{From: src, To: xssOnly},
			{From: xssOnly, To: sink},
		},
		Sources: []Source{{Port: src, Kind: "http"}},
		Sanitizers: []Sanitizer{
			{Port: sqlOnly, Neutralizes: []string{"sql"}},
			{Port: xssOnly, Neutralizes: []string{"xss"}},
		},
		Sinks: []Sink{
			{Port: sink, Kind: "sql"},
			{Port: sink, Kind: "xss"},
		},
	}

	res := Solve(program, DefaultLimits())
	if len(res.Findings) != 2 {
		t.Fatalf("findings = %d, want sql and xss findings: %+v", len(res.Findings), res.Findings)
	}
	for _, finding := range res.Findings {
		switch finding.SinkKind {
		case "sql":
			if reflect.DeepEqual(finding.Trail, []Port{src, sqlOnly, sink}) {
				t.Fatalf("sql finding received sql-sanitized trail: %+v", finding.Trail)
			}
		case "xss":
			if reflect.DeepEqual(finding.Trail, []Port{src, xssOnly, sink}) {
				t.Fatalf("xss finding received xss-sanitized trail: %+v", finding.Trail)
			}
		default:
			t.Fatalf("unexpected sink kind %q: %+v", finding.SinkKind, finding)
		}
	}
}

// TestPartitionedEqualsSerialAtCap proves the identity contract holds at the cap
// boundary, including mixed sink slot kinds (a comparator-tie regression guard).
func TestPartitionedEqualsSerialAtCap(t *testing.T) {
	t.Parallel()

	program := Program{}
	for i := 0; i < 8; i++ {
		s := param(FunctionID("repo\x1fsrc"+string(rune('A'+i))), 0)
		// Alternate sink slot kind so findings can be comparator-tied.
		var k Port
		if i%2 == 0 {
			k = param(FunctionID("repo\x1ff"+string(rune('A'+i))), 0)
		} else {
			k = ret(FunctionID("repo\x1ff" + string(rune('A'+i))))
		}
		program.Edges = append(program.Edges, Edge{From: s, To: k})
		program.Sources = append(program.Sources, Source{Port: s, Kind: "http"})
		program.Sinks = append(program.Sinks, Sink{Port: k, Kind: "sql"})
	}
	limits := Limits{MaxFindings: 3}
	if !reflect.DeepEqual(Solve(program, limits), SolvePartitioned(program, limits)) {
		t.Fatalf("partitioned != serial at cap\n serial=%+v\n part=%+v",
			Solve(program, limits), SolvePartitioned(program, limits))
	}
}

// TestOverflowCounted proves finding emission stops at the cap and counts drops.
func TestOverflowCounted(t *testing.T) {
	t.Parallel()

	res := Solve(multiComponentProgram(5), Limits{MaxFindings: 2})
	if len(res.Findings) != 2 {
		t.Fatalf("emitted %d, want 2 (cap)", len(res.Findings))
	}
	if res.Overflow != 3 {
		t.Fatalf("overflow = %d, want 3", res.Overflow)
	}
}
