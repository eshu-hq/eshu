package reducer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
)

// SolveValueFlowSnapshotIncrementalDurable partitions durable summary/source/
// sink state before program assembly, then assembles and solves only components
// missing from the in-process and durable component caches.
func SolveValueFlowSnapshotIncrementalDurable(
	ctx context.Context,
	effects map[summary.FunctionID]summary.Effects,
	versions map[summary.FunctionID]string,
	sources []interproc.Source,
	sinks []interproc.Sink,
	cache *ValueFlowFixpointCache,
	store ValueFlowFixpointComponentStore,
	limits interproc.Limits,
) (interproc.Result, ValueFlowFixpointCacheStats, error) {
	if cache == nil && store == nil {
		program := valueflow.BuildProgram(effects, sources, sinks)
		components := partitionValueFlowProgram(program)
		return interproc.SolvePartitioned(program, limits), ValueFlowFixpointCacheStats{
			ComponentCount:      len(components),
			AssembledComponents: len(components),
		}, nil
	}
	if cache == nil {
		cache = NewValueFlowFixpointCache()
	}

	components := partitionValueFlowSnapshot(effects, versions, sources, sinks)
	keyed := make([]valueFlowComponentProgram, 0, len(components))
	for _, component := range components {
		keyed = append(keyed, valueFlowComponentProgram{key: component.key})
	}
	durableReused, err := hydrateValueFlowFixpointCache(ctx, keyed, cache, store)
	if err != nil {
		return interproc.Result{}, ValueFlowFixpointCacheStats{}, err
	}

	stats := ValueFlowFixpointCacheStats{
		ComponentCount: len(components),
		DurableReused:  durableReused,
	}
	results := make([]interproc.Result, len(components))
	recomputed := make([]bool, len(components))
	assembled := make([]bool, len(components))

	sem := make(chan struct{}, maxValueFlowInt(1, runtime.GOMAXPROCS(0)))
	var wg sync.WaitGroup
	for idx := range components {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if result, ok := cache.get(components[i].key); ok {
				results[i] = result
				return
			}
			program := components[i].program(effects)
			assembled[i] = true
			result := interproc.Solve(program, interproc.Limits{MaxFindings: math.MaxInt})
			cache.put(components[i].key, result)
			results[i] = result
			recomputed[i] = true
		}(idx)
	}
	wg.Wait()

	findings := make([]interproc.Finding, 0)
	for i, result := range results {
		if assembled[i] {
			stats.AssembledComponents++
		}
		if recomputed[i] {
			stats.RecomputedComponents++
		} else {
			stats.ReusedComponents++
		}
		findings = append(findings, result.Findings...)
	}
	if store != nil {
		entries := make(map[string]interproc.Result)
		for i, wasRecomputed := range recomputed {
			if wasRecomputed {
				entries[components[i].key] = results[i]
			}
		}
		if len(entries) > 0 {
			if err := store.StoreValueFlowFixpointComponents(ctx, entries); err != nil {
				return interproc.Result{}, ValueFlowFixpointCacheStats{}, err
			}
		}
	}
	return capValueFlowFindings(findings, limits), stats, nil
}

type valueFlowSnapshotComponent struct {
	key       string
	functions []summary.FunctionID
	sources   []interproc.Source
	sinks     []interproc.Sink
}

func (c valueFlowSnapshotComponent) program(effects map[summary.FunctionID]summary.Effects) interproc.Program {
	componentEffects := make(map[summary.FunctionID]summary.Effects, len(c.functions))
	for _, id := range c.functions {
		if fx, ok := effects[id]; ok {
			componentEffects[id] = fx
		}
	}
	return valueflow.BuildProgram(componentEffects, c.sources, c.sinks)
}

func partitionValueFlowSnapshot(
	effects map[summary.FunctionID]summary.Effects,
	versions map[summary.FunctionID]string,
	sources []interproc.Source,
	sinks []interproc.Sink,
) []valueFlowSnapshotComponent {
	uf := newValueFlowUnionFind()
	addFunction := func(id summary.FunctionID) {
		if id != "" {
			uf.add(valueFlowFunctionPort(id))
		}
	}
	for id, fx := range effects {
		if len(fx.ParamToReturn) > 0 || len(fx.ParamToSink) > 0 {
			addFunction(id)
		}
		for _, flow := range fx.ParamToCallArg {
			uf.union(valueFlowFunctionPort(id), valueFlowFunctionPort(flow.Callee))
		}
	}
	for _, source := range sources {
		addFunction(summary.FunctionID(source.Port.Func))
	}
	for _, sink := range sinks {
		addFunction(summary.FunctionID(sink.Port.Func))
	}

	byRoot := map[interproc.Port]*valueFlowSnapshotComponent{}
	rootFor := func(id summary.FunctionID) interproc.Port {
		return uf.find(valueFlowFunctionPort(id))
	}
	componentFor := func(id summary.FunctionID) *valueFlowSnapshotComponent {
		root := rootFor(id)
		component := byRoot[root]
		if component == nil {
			component = &valueFlowSnapshotComponent{}
			byRoot[root] = component
		}
		return component
	}
	seenFunctions := map[summary.FunctionID]struct{}{}
	for id, fx := range effects {
		if len(fx.ParamToReturn) > 0 || len(fx.ParamToSink) > 0 || len(fx.ParamToCallArg) > 0 {
			if _, seen := seenFunctions[id]; !seen {
				component := componentFor(id)
				component.functions = append(component.functions, id)
				seenFunctions[id] = struct{}{}
			}
		}
		for _, flow := range fx.ParamToCallArg {
			if flow.Callee == "" {
				continue
			}
			if _, seen := seenFunctions[flow.Callee]; seen {
				continue
			}
			component := componentFor(flow.Callee)
			component.functions = append(component.functions, flow.Callee)
			seenFunctions[flow.Callee] = struct{}{}
		}
	}
	for _, source := range sources {
		id := summary.FunctionID(source.Port.Func)
		componentFor(id).sources = append(componentFor(id).sources, source)
		if _, seen := seenFunctions[id]; !seen {
			componentFor(id).functions = append(componentFor(id).functions, id)
			seenFunctions[id] = struct{}{}
		}
	}
	for _, sink := range sinks {
		id := summary.FunctionID(sink.Port.Func)
		componentFor(id).sinks = append(componentFor(id).sinks, sink)
		if _, seen := seenFunctions[id]; !seen {
			componentFor(id).functions = append(componentFor(id).functions, id)
			seenFunctions[id] = struct{}{}
		}
	}

	roots := make([]interproc.Port, 0, len(byRoot))
	for root := range byRoot {
		roots = append(roots, root)
	}
	sortValueFlowPorts(roots)

	out := make([]valueFlowSnapshotComponent, 0, len(roots))
	for _, root := range roots {
		component := *byRoot[root]
		sort.Slice(component.functions, func(i, j int) bool { return component.functions[i] < component.functions[j] })
		component.key = valueFlowSnapshotComponentKey(component, effects, versions)
		out = append(out, component)
	}
	return out
}

func valueFlowSnapshotComponentKey(
	component valueFlowSnapshotComponent,
	effects map[summary.FunctionID]summary.Effects,
	versions map[summary.FunctionID]string,
) string {
	var b strings.Builder
	b.WriteString(valueFlowFixpointComponentKeyVersion)
	b.WriteByte('\n')
	for _, id := range component.functions {
		b.WriteString("fn:")
		b.WriteString(string(id))
		b.WriteByte('=')
		b.WriteString(versions[id])
		b.WriteByte('\n')
	}
	for _, edge := range sortedValueFlowEdges(valueFlowSnapshotComponentEdges(component, effects)) {
		b.WriteString("edge:")
		writeValueFlowPort(&b, edge.From)
		b.WriteString("->")
		writeValueFlowPort(&b, edge.To)
		b.WriteByte('\n')
	}
	for _, source := range sortedValueFlowSources(component.sources) {
		b.WriteString("source:")
		writeValueFlowPort(&b, source.Port)
		b.WriteByte('|')
		b.WriteString(source.Kind)
		b.WriteByte('|')
		b.WriteString(source.Label)
		b.WriteByte('\n')
	}
	for _, sink := range sortedValueFlowSinks(valueFlowSnapshotComponentSinks(component, effects)) {
		b.WriteString("sink:")
		writeValueFlowPort(&b, sink.Port)
		b.WriteByte('|')
		b.WriteString(sink.Kind)
		b.WriteByte('|')
		b.WriteString(sink.Label)
		b.WriteByte('|')
		b.WriteString(strconv.FormatBool(sink.Cloud))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func valueFlowSnapshotComponentEdges(
	component valueFlowSnapshotComponent,
	effects map[summary.FunctionID]summary.Effects,
) []interproc.Edge {
	var edges []interproc.Edge
	for _, id := range component.functions {
		fx, ok := effects[id]
		if !ok {
			continue
		}
		for _, flow := range fx.ParamToCallArg {
			edges = append(edges, interproc.Edge{
				From: valueFlowSummaryParamPort(id, flow.Param),
				To:   valueFlowSummaryParamPort(flow.Callee, flow.Arg),
			})
		}
		for _, param := range fx.ParamToReturn {
			edges = append(edges, interproc.Edge{
				From: valueFlowSummaryParamPort(id, param),
				To:   valueFlowReturnPort(id),
			})
		}
	}
	return edges
}

func valueFlowSnapshotComponentSinks(
	component valueFlowSnapshotComponent,
	effects map[summary.FunctionID]summary.Effects,
) []interproc.Sink {
	sinks := append([]interproc.Sink(nil), component.sinks...)
	for _, id := range component.functions {
		fx, ok := effects[id]
		if !ok {
			continue
		}
		for _, sink := range fx.ParamToSink {
			sinks = append(sinks, interproc.Sink{
				Port: valueFlowSummaryParamPort(id, sink.Param),
				Kind: sink.SinkKind,
			})
		}
	}
	return sinks
}

func valueFlowFunctionPort(id summary.FunctionID) interproc.Port {
	return interproc.Port{Func: interproc.FunctionID(id)}
}

func valueFlowSummaryParamPort(id summary.FunctionID, index int) interproc.Port {
	return interproc.Port{Func: interproc.FunctionID(id), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: index}}
}

func valueFlowReturnPort(id summary.FunctionID) interproc.Port {
	return interproc.Port{Func: interproc.FunctionID(id), Slot: interproc.Slot{Kind: interproc.SlotReturn}}
}

func sortValueFlowPorts(ports []interproc.Port) {
	sort.Slice(ports, func(i, j int) bool { return valueFlowPortLess(ports[i], ports[j]) })
}
