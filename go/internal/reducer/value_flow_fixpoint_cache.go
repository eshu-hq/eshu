package reducer

import (
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
)

// ValueFlowFixpointCache stores solved weak components of the value-flow
// program. It is safe for concurrent reducer workers.
type ValueFlowFixpointCache struct {
	mu      sync.Mutex
	entries map[string]interproc.Result
}

// ValueFlowFixpointCacheStats reports how much of a solve reused cached
// component results.
type ValueFlowFixpointCacheStats struct {
	ComponentCount       int
	RecomputedComponents int
	ReusedComponents     int
}

// NewValueFlowFixpointCache returns an empty concurrency-safe fixpoint cache.
func NewValueFlowFixpointCache() *ValueFlowFixpointCache {
	return &ValueFlowFixpointCache{entries: map[string]interproc.Result{}}
}

// SolveValueFlowProgramIncremental solves a value-flow program by weak
// component, reusing cached component findings when the component content and
// function content versions are unchanged. The merged result is sorted and
// capped identically to the full partitioned solve.
func SolveValueFlowProgramIncremental(
	program interproc.Program,
	versions map[summary.FunctionID]string,
	cache *ValueFlowFixpointCache,
	limits interproc.Limits,
) (interproc.Result, ValueFlowFixpointCacheStats) {
	if cache == nil {
		return interproc.SolvePartitioned(program, limits), ValueFlowFixpointCacheStats{}
	}

	components := partitionValueFlowProgram(program)
	stats := ValueFlowFixpointCacheStats{ComponentCount: len(components)}
	results := make([]interproc.Result, len(components))
	recomputed := make([]bool, len(components))

	sem := make(chan struct{}, maxValueFlowInt(1, runtime.GOMAXPROCS(0)))
	var wg sync.WaitGroup
	for idx := range components {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			key := valueFlowComponentKey(components[i], versions)
			if result, ok := cache.get(key); ok {
				results[i] = result
				return
			}
			result := interproc.Solve(components[i], interproc.Limits{MaxFindings: math.MaxInt})
			cache.put(key, result)
			results[i] = result
			recomputed[i] = true
		}(idx)
	}
	wg.Wait()

	findings := make([]interproc.Finding, 0)
	for i, result := range results {
		if recomputed[i] {
			stats.RecomputedComponents++
		} else {
			stats.ReusedComponents++
		}
		findings = append(findings, result.Findings...)
	}
	return capValueFlowFindings(findings, limits), stats
}

func (c *ValueFlowFixpointCache) get(key string) (interproc.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result, ok := c.entries[key]
	return result, ok
}

func (c *ValueFlowFixpointCache) put(key string, result interproc.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = result
}

func capValueFlowFindings(findings []interproc.Finding, limits interproc.Limits) interproc.Result {
	maxFindings := limits.MaxFindings
	if maxFindings <= 0 {
		maxFindings = interproc.DefaultLimits().MaxFindings
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return valueFlowFindingLess(findings[i], findings[j])
	})
	if len(findings) <= maxFindings {
		return interproc.Result{Findings: findings}
	}
	return interproc.Result{Findings: findings[:maxFindings], Overflow: len(findings) - maxFindings}
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
	for _, id := range valueFlowComponentFunctionIDs(program) {
		b.WriteString("fn:")
		b.WriteString(string(id))
		b.WriteByte('=')
		b.WriteString(versions[id])
		b.WriteByte('\n')
	}
	for _, source := range sortedValueFlowSources(program.Sources) {
		b.WriteString("source:")
		writeValueFlowPort(&b, source.Port)
		b.WriteByte('|')
		b.WriteString(source.Kind)
		b.WriteByte('|')
		b.WriteString(source.Label)
		b.WriteByte('\n')
	}
	for _, sink := range sortedValueFlowSinks(program.Sinks) {
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
	for _, sanitizer := range sortedValueFlowSanitizers(program.Sanitizers) {
		b.WriteString("sanitizer:")
		writeValueFlowPort(&b, sanitizer.Port)
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
	return sortedValueFlowFunctionIDs(seen)
}

func partitionValueFlowProgram(program interproc.Program) []interproc.Program {
	uf := newValueFlowUnionFind()
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

func newValueFlowUnionFind() *valueFlowUnionFind {
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

func sortedValueFlowSources(sources []interproc.Source) []interproc.Source {
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

func sortedValueFlowSinks(sinks []interproc.Sink) []interproc.Sink {
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

func sortedValueFlowSanitizers(sanitizers []interproc.Sanitizer) []interproc.Sanitizer {
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

func writeValueFlowPort(b *strings.Builder, port interproc.Port) {
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

func maxValueFlowInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
