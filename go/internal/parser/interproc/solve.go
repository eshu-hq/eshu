package interproc

import (
	"runtime"
	"sort"
	"sync"
)

const maxFindingTrailPorts = 64

// Solve propagates taint over the whole program and returns the bounded,
// deterministic findings.
func Solve(program Program, limits Limits) Result {
	return capFindings(solveAll(program), limits.normalized())
}

// SolvePartitioned splits the program into weakly-connected components (the
// conflict key), solves each independently and concurrently, and merges the
// findings. Taint cannot cross a weakly-connected component boundary, so the
// partition is correct; each component is solved by a pure function over its own
// state, so concurrent execution is race-free and needs no serialization. The
// result is identical to Solve.
func SolvePartitioned(program Program, limits Limits) Result {
	components := partition(program)
	merged := make([][]Finding, len(components))

	sem := make(chan struct{}, max(1, runtime.GOMAXPROCS(0)))
	var wg sync.WaitGroup
	for i := range components {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			merged[idx] = solveAll(components[idx])
		}(i)
	}
	wg.Wait()

	var all []Finding
	for _, findings := range merged {
		all = append(all, findings...)
	}
	return capFindings(all, limits.normalized())
}

// stateKey is a taint state tracked per (port, originating source). Tracking
// taint per source means two different sources reaching the same sink each yield
// a finding with the correct origin, and the neutralized set is intersected only
// across paths from the same source.
type stateKey struct {
	Port   Port
	Source Port
}

type trailState struct {
	Ports     []Port
	Truncated bool
}

// solveAll runs the taint fixpoint and returns every source-to-sink finding,
// unsorted and uncapped. It is a pure function of program, so it is safe to run
// concurrently on disjoint programs.
func solveAll(program Program) []Finding {
	adjacency := map[Port][]Port{}
	for _, edge := range program.Edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}
	for from := range adjacency {
		sort.Slice(adjacency[from], func(i, j int) bool {
			return portLess(adjacency[from][i], adjacency[from][j])
		})
	}
	sanitizers := map[Port]map[string]struct{}{}
	for _, san := range program.Sanitizers {
		kinds := sanitizers[san.Port]
		if kinds == nil {
			kinds = map[string]struct{}{}
			sanitizers[san.Port] = kinds
		}
		for _, k := range san.Neutralizes {
			kinds[k] = struct{}{}
		}
	}
	sinkKinds := map[string]struct{}{}
	for _, sink := range program.Sinks {
		sinkKinds[sink.Kind] = struct{}{}
	}

	states := map[stateKey]map[string]struct{}{}
	witnesses := map[stateKey]map[string]trailState{}
	sourceByPort := map[Port]Source{}
	var work []stateKey

	// Seed sources in a deterministic order; the first source at a port wins its
	// label attribution.
	sources := append([]Source(nil), program.Sources...)
	sort.Slice(sources, func(i, j int) bool { return portLess(sources[i].Port, sources[j].Port) })
	for _, src := range sources {
		if _, seen := sourceByPort[src.Port]; !seen {
			sourceByPort[src.Port] = src
		}
		key := stateKey{Port: src.Port, Source: src.Port}
		incoming := withSanitizer(map[string]struct{}{}, sanitizers[src.Port])
		if mergeState(states, key, incoming) {
			seedWitnesses(witnesses, key, src.Port, incoming, sinkKinds)
			work = append(work, key)
		}
	}

	for len(work) > 0 {
		from := work[0]
		work = work[1:]
		outgoing := states[from]
		for _, to := range adjacency[from.Port] {
			next := stateKey{Port: to, Source: from.Source}
			incoming := withSanitizer(outgoing, sanitizers[to])
			stateChanged := mergeState(states, next, incoming)
			witnessChanged := mergeWitnesses(witnesses, from, next, to, incoming, sinkKinds)
			if stateChanged || witnessChanged {
				work = append(work, next)
			}
		}
	}

	var findings []Finding
	for _, sink := range program.Sinks {
		for srcPort, source := range sourceByPort {
			neutralized, reached := states[stateKey{Port: sink.Port, Source: srcPort}]
			if !reached {
				continue
			}
			if _, suppressed := neutralized[sink.Kind]; suppressed {
				continue
			}
			key := stateKey{Port: sink.Port, Source: srcPort}
			findings = append(findings, Finding{
				SourceFunc:     source.Port.Func,
				SourceKind:     source.Kind,
				SourceLabel:    source.Label,
				SinkFunc:       sink.Port.Func,
				SinkKind:       sink.Kind,
				SinkLabel:      sink.Label,
				SinkPort:       sink.Port,
				Cloud:          sink.Cloud,
				Neutralized:    sortedStrings(neutralized),
				Confidence:     interprocConfidence,
				Trail:          clonePorts(witnesses[key][sink.Kind].Ports),
				TrailTruncated: witnesses[key][sink.Kind].Truncated,
			})
		}
	}
	return findings
}

// mergeState folds an incoming tainted path (for one source) into a (port,
// source) state. The first path establishes the neutralized set; later paths
// from the same source intersect it so a kind survives only when every path
// neutralized it.
func mergeState(states map[stateKey]map[string]struct{}, key stateKey, incoming map[string]struct{}) bool {
	existing, ok := states[key]
	if !ok {
		states[key] = cloneStrings(incoming)
		return true
	}
	changed := false
	for k := range existing {
		if _, present := incoming[k]; !present {
			delete(existing, k)
			changed = true
		}
	}
	return changed
}

func seedWitnesses(
	witnesses map[stateKey]map[string]trailState,
	key stateKey,
	port Port,
	neutralized map[string]struct{},
	sinkKinds map[string]struct{},
) {
	for kind := range sinkKinds {
		if _, suppressed := neutralized[kind]; suppressed {
			continue
		}
		addWitness(witnesses, key, kind, trailState{Ports: []Port{port}})
	}
}

func mergeWitnesses(
	witnesses map[stateKey]map[string]trailState,
	from stateKey,
	next stateKey,
	port Port,
	neutralized map[string]struct{},
	sinkKinds map[string]struct{},
) bool {
	changed := false
	for kind := range sinkKinds {
		if _, suppressed := neutralized[kind]; suppressed {
			continue
		}
		base, ok := witnesses[from][kind]
		if !ok {
			continue
		}
		if addWitness(witnesses, next, kind, appendTrailPort(base, port)) {
			changed = true
		}
	}
	return changed
}

func addWitness(witnesses map[stateKey]map[string]trailState, key stateKey, kind string, trail trailState) bool {
	byKind := witnesses[key]
	if byKind == nil {
		byKind = map[string]trailState{}
		witnesses[key] = byKind
	}
	if _, exists := byKind[kind]; exists {
		return false
	}
	byKind[kind] = trailState{Ports: clonePorts(trail.Ports), Truncated: trail.Truncated}
	return true
}

func appendTrailPort(base trailState, next Port) trailState {
	if len(base.Ports) == 0 {
		return trailState{Ports: []Port{next}, Truncated: base.Truncated}
	}
	out := clonePorts(base.Ports)
	if len(out) >= maxFindingTrailPorts {
		out[len(out)-1] = next
		return trailState{Ports: out, Truncated: true}
	}
	out = append(out, next)
	return trailState{Ports: out, Truncated: base.Truncated}
}

func clonePorts(in []Port) []Port {
	if len(in) == 0 {
		return nil
	}
	out := make([]Port, len(in))
	copy(out, in)
	return out
}

// withSanitizer returns base unioned with a port's neutralized kinds.
func withSanitizer(base, add map[string]struct{}) map[string]struct{} {
	out := cloneStrings(base)
	for k := range add {
		out[k] = struct{}{}
	}
	return out
}

// capFindings sorts findings, truncates to the limit, and counts the overflow.
func capFindings(findings []Finding, limits Limits) Result {
	sortFindings(findings)
	if len(findings) <= limits.MaxFindings {
		return Result{Findings: findings}
	}
	return Result{Findings: findings[:limits.MaxFindings], Overflow: len(findings) - limits.MaxFindings}
}

func cloneStrings(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func sortedStrings(in map[string]struct{}) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// portLess orders ports deterministically.
func portLess(a, b Port) bool {
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
