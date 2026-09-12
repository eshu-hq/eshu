// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// FixpointCache stores solved weak components of the value-flow
// program. It is safe for concurrent reducer workers.
type FixpointCache struct {
	mu      sync.Mutex
	entries map[string]interproc.Result
}

const valueFlowFixpointComponentKeyVersion = "value-flow-fixpoint-component:v1"

// FixpointCacheStats reports how much of a solve reused cached
// component results.
type FixpointCacheStats struct {
	ComponentCount       int
	AssembledComponents  int
	RecomputedComponents int
	ReusedComponents     int
	DurableReused        int
}

// FixpointComponentStore persists solved component results across
// reducer process restarts and replicas.
type FixpointComponentStore interface {
	LoadValueFlowFixpointComponents(ctx context.Context, keys []string) (map[string]interproc.Result, error)
	StoreValueFlowFixpointComponents(ctx context.Context, entries map[string]interproc.Result) error
}

// NewFixpointCache returns an empty concurrency-safe fixpoint cache.
func NewFixpointCache() *FixpointCache {
	return &FixpointCache{entries: map[string]interproc.Result{}}
}

// SolveProgramIncremental solves a value-flow program by weak
// component, reusing cached component findings when the component content and
// function content versions are unchanged. The merged result is sorted and
// capped identically to the full partitioned solve.
func SolveProgramIncremental(
	program interproc.Program,
	versions map[summary.FunctionID]string,
	cache *FixpointCache,
	limits interproc.Limits,
) (interproc.Result, FixpointCacheStats) {
	result, stats, err := SolveProgramIncrementalDurable(context.Background(), program, versions, cache, nil, limits)
	if err != nil {
		return interproc.SolvePartitioned(program, limits), FixpointCacheStats{}
	}
	return result, stats
}

// SolveProgramIncrementalDurable solves value-flow components while
// consulting an optional durable component-result store before recomputing.
func SolveProgramIncrementalDurable(
	ctx context.Context,
	program interproc.Program,
	versions map[summary.FunctionID]string,
	cache *FixpointCache,
	store FixpointComponentStore,
	limits interproc.Limits,
) (interproc.Result, FixpointCacheStats, error) {
	if cache == nil && store == nil {
		return interproc.SolvePartitioned(program, limits), FixpointCacheStats{}, nil
	}
	if cache == nil {
		cache = NewFixpointCache()
	}
	components := partitionProgram(program)
	keyed := make([]valueFlowComponentProgram, 0, len(components))
	for _, component := range components {
		keyed = append(keyed, valueFlowComponentProgram{
			key:     valueFlowComponentKey(component, versions),
			program: component,
		})
	}
	durableReused, err := hydrateFixpointCache(ctx, keyed, cache, store)
	if err != nil {
		return interproc.Result{}, FixpointCacheStats{}, err
	}

	stats := FixpointCacheStats{
		ComponentCount:      len(components),
		AssembledComponents: len(components),
	}
	stats.DurableReused = durableReused
	results := make([]interproc.Result, len(components))
	recomputed := make([]bool, len(components))

	sem := make(chan struct{}, cpubudget.UsableCPUs())
	var wg sync.WaitGroup
	for idx := range keyed {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if result, ok := cache.get(keyed[i].key); ok {
				results[i] = result
				return
			}
			result := boundedComponentResult(
				interproc.Solve(keyed[i].program, interproc.Limits{MaxFindings: math.MaxInt}),
				limits,
			)
			if cache != nil {
				cache.put(keyed[i].key, result)
			}
			results[i] = result
			recomputed[i] = true
		}(idx)
	}
	wg.Wait()

	findings := make([]interproc.Finding, 0)
	overflow := 0
	for i, result := range results {
		if recomputed[i] {
			stats.RecomputedComponents++
		} else {
			stats.ReusedComponents++
		}
		findings = append(findings, result.Findings...)
		overflow += result.Overflow
	}
	if store != nil {
		entries := make(map[string]interproc.Result)
		for i, wasRecomputed := range recomputed {
			if wasRecomputed {
				entries[keyed[i].key] = results[i]
			}
		}
		if len(entries) > 0 {
			if err := store.StoreValueFlowFixpointComponents(ctx, entries); err != nil {
				return interproc.Result{}, FixpointCacheStats{}, err
			}
		}
	}
	return capFindingsWithOverflow(findings, limits, overflow), stats, nil
}

type valueFlowComponentProgram struct {
	key     string
	program interproc.Program
}

func hydrateFixpointCache(
	ctx context.Context,
	components []valueFlowComponentProgram,
	cache *FixpointCache,
	store FixpointComponentStore,
) (int, error) {
	if cache == nil || store == nil || len(components) == 0 {
		return 0, nil
	}
	missing := make([]string, 0, len(components))
	seenMissing := map[string]struct{}{}
	for _, component := range components {
		if _, ok := cache.get(component.key); ok {
			continue
		}
		if _, seen := seenMissing[component.key]; seen {
			continue
		}
		seenMissing[component.key] = struct{}{}
		missing = append(missing, component.key)
	}
	if len(missing) == 0 {
		return 0, nil
	}
	entries, err := store.LoadValueFlowFixpointComponents(ctx, missing)
	if err != nil {
		return 0, err
	}
	for key, result := range entries {
		cache.put(key, result)
	}
	return len(entries), nil
}

func (c *FixpointCache) get(key string) (interproc.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.entries[key]
	return result, ok
}

func (c *FixpointCache) put(key string, result interproc.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = result
}

func boundedComponentResult(result interproc.Result, limits interproc.Limits) interproc.Result {
	return capFindingsWithOverflow(result.Findings, limits, result.Overflow)
}

func capFindingsWithOverflow(findings []interproc.Finding, limits interproc.Limits, overflow int) interproc.Result {
	maxFindings := limits.MaxFindings
	if maxFindings <= 0 {
		maxFindings = interproc.DefaultLimits().MaxFindings
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return valueFlowFindingLess(findings[i], findings[j])
	})
	if len(findings) <= maxFindings {
		return interproc.Result{Findings: findings, Overflow: overflow}
	}
	return interproc.Result{Findings: findings[:maxFindings], Overflow: overflow + len(findings) - maxFindings}
}

func valueFlowFindingLess(a, b interproc.Finding) bool {
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

func valueFlowComponentKey(program interproc.Program, versions map[summary.FunctionID]string) string {
	var b strings.Builder
	b.WriteString(valueFlowFixpointComponentKeyVersion)
	b.WriteByte('\n')
	for _, id := range valueFlowComponentFunctionIDs(program) {
		b.WriteString("fn:")
		b.WriteString(string(id))
		b.WriteByte('=')
		b.WriteString(versions[id])
		b.WriteByte('\n')
	}
	for _, edge := range sortedEdges(program.Edges) {
		b.WriteString("edge:")
		writePort(&b, edge.From)
		b.WriteString("->")
		writePort(&b, edge.To)
		b.WriteByte('\n')
	}
	for _, source := range sortedSources(program.Sources) {
		b.WriteString("source:")
		writePort(&b, source.Port)
		b.WriteByte('|')
		b.WriteString(source.Kind)
		b.WriteByte('|')
		b.WriteString(source.Label)
		b.WriteByte('\n')
	}
	for _, sink := range sortedSinks(program.Sinks) {
		b.WriteString("sink:")
		writePort(&b, sink.Port)
		b.WriteByte('|')
		b.WriteString(sink.Kind)
		b.WriteByte('|')
		b.WriteString(sink.Label)
		b.WriteByte('|')
		b.WriteString(strconv.FormatBool(sink.Cloud))
		b.WriteByte('\n')
	}
	for _, sanitizer := range sortedSanitizers(program.Sanitizers) {
		b.WriteString("sanitizer:")
		writePort(&b, sanitizer.Port)
		for _, kind := range sortedStrings(sanitizer.Neutralizes) {
			b.WriteByte('|')
			b.WriteString(kind)
		}
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func valueFlowComponentFunctionIDs(program interproc.Program) []summary.FunctionID {
	seen := map[summary.FunctionID]struct{}{}
	add := func(fn interproc.FunctionID) {
		if fn != "" {
			seen[summary.FunctionID(fn)] = struct{}{}
		}
	}
	for _, edge := range program.Edges {
		add(edge.From.Func)
		add(edge.To.Func)
	}
	for _, source := range program.Sources {
		add(source.Port.Func)
	}
	for _, sink := range program.Sinks {
		add(sink.Port.Func)
	}
	for _, sanitizer := range program.Sanitizers {
		add(sanitizer.Port.Func)
	}
	return sortedFunctionIDs(seen)
}

func sortedEdges(edges []interproc.Edge) []interproc.Edge {
	out := append([]interproc.Edge(nil), edges...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return valueFlowPortLess(out[i].From, out[j].From)
		}
		return valueFlowPortLess(out[i].To, out[j].To)
	})
	return out
}

func partitionProgram(program interproc.Program) []interproc.Program {
	uf := newUnionFind()
	for _, edge := range program.Edges {
		uf.union(edge.From, edge.To)
	}
	for _, source := range program.Sources {
		uf.add(source.Port)
	}
	for _, sink := range program.Sinks {
		uf.add(sink.Port)
	}
	for _, sanitizer := range program.Sanitizers {
		uf.add(sanitizer.Port)
	}

	byRoot := map[interproc.Port]*interproc.Program{}
	roots := make([]interproc.Port, 0)
	get := func(port interproc.Port) *interproc.Program {
		root := uf.find(port)
		component := byRoot[root]
		if component == nil {
			component = &interproc.Program{}
			byRoot[root] = component
			roots = append(roots, root)
		}
		return component
	}
	for _, edge := range program.Edges {
		component := get(edge.From)
		component.Edges = append(component.Edges, edge)
	}
	for _, source := range program.Sources {
		component := get(source.Port)
		component.Sources = append(component.Sources, source)
	}
	for _, sink := range program.Sinks {
		component := get(sink.Port)
		component.Sinks = append(component.Sinks, sink)
	}
	for _, sanitizer := range program.Sanitizers {
		component := get(sanitizer.Port)
		component.Sanitizers = append(component.Sanitizers, sanitizer)
	}

	sort.Slice(roots, func(i, j int) bool { return valueFlowPortLess(roots[i], roots[j]) })
	out := make([]interproc.Program, 0, len(roots))
	for _, root := range roots {
		out = append(out, *byRoot[root])
	}
	return out
}

type valueFlowUnionFind struct {
	parent map[interproc.Port]interproc.Port
	size   map[interproc.Port]int
}

func newUnionFind() *valueFlowUnionFind {
	return &valueFlowUnionFind{parent: map[interproc.Port]interproc.Port{}, size: map[interproc.Port]int{}}
}

func (u *valueFlowUnionFind) add(port interproc.Port) {
	if _, ok := u.parent[port]; !ok {
		u.parent[port] = port
		u.size[port] = 1
	}
}

func (u *valueFlowUnionFind) find(port interproc.Port) interproc.Port {
	u.add(port)
	for u.parent[port] != port {
		u.parent[port] = u.parent[u.parent[port]]
		port = u.parent[port]
	}
	return port
}

func (u *valueFlowUnionFind) union(a, b interproc.Port) {
	ra := u.find(a)
	rb := u.find(b)
	if ra == rb {
		return
	}
	if u.size[ra] < u.size[rb] || (u.size[ra] == u.size[rb] && valueFlowPortLess(rb, ra)) {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	u.size[ra] += u.size[rb]
}

func sortedSources(sources []interproc.Source) []interproc.Source {
	out := append([]interproc.Source(nil), sources...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return valueFlowPortLess(out[i].Port, out[j].Port)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func sortedSinks(sinks []interproc.Sink) []interproc.Sink {
	out := append([]interproc.Sink(nil), sinks...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return valueFlowPortLess(out[i].Port, out[j].Port)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return !out[i].Cloud && out[j].Cloud
	})
	return out
}

func sortedSanitizers(sanitizers []interproc.Sanitizer) []interproc.Sanitizer {
	out := append([]interproc.Sanitizer(nil), sanitizers...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return valueFlowPortLess(out[i].Port, out[j].Port)
		}
		return strings.Join(sortedStrings(out[i].Neutralizes), "\x00") < strings.Join(sortedStrings(out[j].Neutralizes), "\x00")
	})
	return out
}

func valueFlowPortLess(a, b interproc.Port) bool {
	if a.Func != b.Func {
		return a.Func < b.Func
	}
	if a.Slot.Kind != b.Slot.Kind {
		return a.Slot.Kind < b.Slot.Kind
	}
	if a.Slot.Index != b.Slot.Index {
		return a.Slot.Index < b.Slot.Index
	}
	return a.Slot.Name < b.Slot.Name
}

func writePort(b *strings.Builder, port interproc.Port) {
	b.WriteString(string(port.Func))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(port.Slot.Kind)))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(port.Slot.Index))
	b.WriteByte(':')
	b.WriteString(port.Slot.Name)
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
