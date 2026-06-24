// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package valueflow

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// TestDeriveParamToSink proves a parameter that flows to a sink yields a
// ParamToSink effect, and a sanitizer for that kind removes it.
func TestDeriveParamToSink(t *testing.T) {
	t.Parallel()

	build := func() (cfg.Function, int, int) {
		b := cfg.NewBuilder(cfg.DefaultLimits())
		blk := b.AddBlock()
		s0 := int(b.AddStmt(blk, 1, []string{"arg"}, nil))
		s1 := int(b.AddStmt(blk, 2, nil, []string{"arg"}))
		return b.Build(), s0, s1
	}

	fn, s0, s1 := build()
	eff := DeriveEffects(fn, EffectsSpec{
		Params: []ParamSlot{{Index: 0, Stmt: s0, Binding: "arg"}},
		Sinks:  map[int]SinkSlot{s1: {Kind: "sql"}},
	})
	want := []summary.ParamSink{{Param: 0, SinkKind: "sql"}}
	if !reflect.DeepEqual(eff.ParamToSink, want) {
		t.Fatalf("ParamToSink = %+v, want %+v", eff.ParamToSink, want)
	}

	// safe = escape(arg) sanitizes for sql, then db.Query(safe): no ParamToSink.
	sb := cfg.NewBuilder(cfg.DefaultLimits())
	sblk := sb.AddBlock()
	ss0 := int(sb.AddStmt(sblk, 1, []string{"arg"}, nil))
	ss1 := int(sb.AddStmt(sblk, 2, []string{"safe"}, []string{"arg"})) // safe = escape(arg)
	ss2 := int(sb.AddStmt(sblk, 3, nil, []string{"safe"}))             // db.Query(safe)
	sanitized := DeriveEffects(sb.Build(), EffectsSpec{
		Params:     []ParamSlot{{Index: 0, Stmt: ss0, Binding: "arg"}},
		Sinks:      map[int]SinkSlot{ss2: {Kind: "sql"}},
		Sanitizers: map[int][]string{ss1: {"sql"}},
	})
	if len(sanitized.ParamToSink) != 0 {
		t.Fatalf("sanitized ParamToSink = %+v, want none", sanitized.ParamToSink)
	}
}

// TestDeriveParamToCallArg proves a parameter flowing into a call argument yields
// a ParamToCallArg effect naming the callee and argument index.
func TestDeriveParamToCallArg(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"in"}, nil))
	s1 := int(b.AddStmt(blk, 2, nil, []string{"in"}))
	fn := b.Build()

	eff := DeriveEffects(fn, EffectsSpec{
		Params:   []ParamSlot{{Index: 0, Stmt: s0, Binding: "in"}},
		CallArgs: []CallArgSlot{{Stmt: s1, Binding: "in", Callee: "repo\x1fquery", Arg: 0}},
	})
	want := []summary.CallArgFlow{{Callee: "repo\x1fquery", Param: 0, Arg: 0}}
	if !reflect.DeepEqual(eff.ParamToCallArg, want) {
		t.Fatalf("ParamToCallArg = %+v, want %+v", eff.ParamToCallArg, want)
	}
}

// TestDeriveMultiRoleStatement proves a statement that is both a return and a
// call-argument (return query(arg)) yields both effects — neither is dropped by
// a single-mark sink map.
func TestDeriveMultiRoleStatement(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"arg"}, nil))
	s1 := int(b.AddStmt(blk, 2, nil, []string{"arg"})) // return query(arg)
	fn := b.Build()

	eff := DeriveEffects(fn, EffectsSpec{
		Params:   []ParamSlot{{Index: 0, Stmt: s0, Binding: "arg"}},
		Returns:  []int{s1},
		CallArgs: []CallArgSlot{{Stmt: s1, Binding: "arg", Callee: "repo\x1fquery", Arg: 0}},
	})
	if !reflect.DeepEqual(eff.ParamToReturn, []int{0}) {
		t.Fatalf("ParamToReturn = %+v, want [0]", eff.ParamToReturn)
	}
	want := []summary.CallArgFlow{{Callee: "repo\x1fquery", Param: 0, Arg: 0}}
	if !reflect.DeepEqual(eff.ParamToCallArg, want) {
		t.Fatalf("ParamToCallArg = %+v, want %+v", eff.ParamToCallArg, want)
	}
}

// TestInterproceduralPipeline proves the full bridge: derive each function's
// effects from its CFG, store them, build the interprocedural program, and solve
// — finding a source in one function reaching a sink in a callee.
func TestInterproceduralPipeline(t *testing.T) {
	t.Parallel()

	const handler = summary.FunctionID("repo\x1fhandler")
	const query = summary.FunctionID("repo\x1fquery")

	// query(arg): arg flows to a sql sink.
	qb := cfg.NewBuilder(cfg.DefaultLimits())
	qblk := qb.AddBlock()
	qs0 := int(qb.AddStmt(qblk, 1, []string{"arg"}, nil))
	qs1 := int(qb.AddStmt(qblk, 2, nil, []string{"arg"}))
	queryEffects := DeriveEffects(qb.Build(), EffectsSpec{
		Params: []ParamSlot{{Index: 0, Stmt: qs0, Binding: "arg"}},
		Sinks:  map[int]SinkSlot{qs1: {Kind: "sql"}},
	})

	// handler(in): in flows into query's argument 0.
	hb := cfg.NewBuilder(cfg.DefaultLimits())
	hblk := hb.AddBlock()
	hs0 := int(hb.AddStmt(hblk, 1, []string{"in"}, nil))
	hs1 := int(hb.AddStmt(hblk, 2, nil, []string{"in"}))
	handlerEffects := DeriveEffects(hb.Build(), EffectsSpec{
		Params:   []ParamSlot{{Index: 0, Stmt: hs0, Binding: "in"}},
		CallArgs: []CallArgSlot{{Stmt: hs1, Binding: "in", Callee: query, Arg: 0}},
	})

	store := summary.NewStore()
	store.Upsert(map[summary.FunctionID]summary.Effects{
		handler: handlerEffects,
		query:   queryEffects,
	})

	program := BuildProgram(
		map[summary.FunctionID]summary.Effects{handler: handlerEffects, query: queryEffects},
		[]interproc.Source{{
			Port: interproc.Port{Func: interproc.FunctionID(handler), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0}},
			Kind: "http",
		}},
		nil,
	)

	res := interproc.Solve(program, interproc.DefaultLimits())
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %d, want 1 (handler->query sql); %+v", len(res.Findings), res.Findings)
	}
	f := res.Findings[0]
	if f.SourceFunc != interproc.FunctionID(handler) || f.SinkFunc != interproc.FunctionID(query) || f.SinkKind != "sql" {
		t.Fatalf("unexpected interprocedural finding: %+v", f)
	}
}

// TestDeriveCallArgSameBindingMultipleArgs proves that when one tainted binding
// occupies more than one argument position on a call (query(in, in)), every
// position yields a ParamToCallArg flow, not just the last.
func TestDeriveCallArgSameBindingMultipleArgs(t *testing.T) {
	t.Parallel()

	b := cfg.NewBuilder(cfg.DefaultLimits())
	blk := b.AddBlock()
	s0 := int(b.AddStmt(blk, 1, []string{"in"}, nil))
	s1 := int(b.AddStmt(blk, 2, nil, []string{"in"})) // query(in, in)
	fn := b.Build()

	eff := DeriveEffects(fn, EffectsSpec{
		Params: []ParamSlot{{Index: 0, Stmt: s0, Binding: "in"}},
		CallArgs: []CallArgSlot{
			{Stmt: s1, Binding: "in", Callee: "repo\x1fquery", Arg: 0},
			{Stmt: s1, Binding: "in", Callee: "repo\x1fquery", Arg: 1},
		},
	})
	want := []summary.CallArgFlow{
		{Callee: "repo\x1fquery", Param: 0, Arg: 0},
		{Callee: "repo\x1fquery", Param: 0, Arg: 1},
	}
	if !reflect.DeepEqual(eff.ParamToCallArg, want) {
		t.Fatalf("ParamToCallArg = %+v, want both arg positions %+v", eff.ParamToCallArg, want)
	}
}
