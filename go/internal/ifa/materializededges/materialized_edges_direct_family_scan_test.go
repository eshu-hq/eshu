// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"sort"
)

// cypherPortClassification is one reducer graph-write port's verdict, derived
// from the Cypher the port reaches rather than from its name.
type cypherPortClassification struct {
	// Port is the reducer interface method name.
	Port string
	// Impl is the repo-relative file declaring the cypher-package method that
	// implements it.
	Impl string
	// WritesEdges is true when the port reaches at least one Cypher template
	// containing a relationship MERGE or CREATE.
	WritesEdges bool
	// Evidence is the first relationship-merging line the port reaches, so a
	// failure report can name the write site instead of asserting one exists.
	Evidence string
	// UnknownRefs names dynamic calls or unresolved package string references
	// that prevent a node-only conclusion for this port.
	UnknownRefs []string
}

// cypherPackageSource is the parsed go/internal/storage/cypher package: the
// string constants it declares and the identifier references of every function
// and method body, which together let a port be followed to the Cypher it
// reaches.
type cypherPackageSource struct {
	// configurations keeps valid build-tag variants separate. Reachability is
	// evaluated within each variant before their final port verdicts are joined.
	configurations []*cypherPackageSource
	// bodyCalls maps a function key to the exact package-local function and
	// method objects its body calls.
	bodyCalls map[string][]string
	// bodyUnknownRefs maps a function key to calls or string references the
	// static scan cannot resolve safely.
	bodyUnknownRefs map[string][]string
	// rootKeysByPort holds only methods whose signatures match reducer ports.
	rootKeysByPort map[string][]string
	// bodyLiterals maps a function key to string literals written directly in
	// its body, including function-local consts.
	bodyLiterals map[string][]string
	// keysByName supports standalone scan fixtures that do not load reducer
	// types. Production roots and all transitive calls use exact type objects.
	keysByName map[string][]string
	// fileByKey maps a function key to the repo-relative file declaring it.
	fileByKey map[string]string
}

// classifyCypherPorts returns, for every reducer interface port implemented in
// the cypher package, whether that port reaches a relationship MERGE.
//
// "Reaches" is followed transitively through package-local calls, because the
// write site is routinely two hops from the port: WriteSemanticEntities calls
// semanticEntityPlans, which names the upsert templates that carry the
// CONTAINS merge. A one-hop scan classifies that port as node-only, which is
// precisely the miss this guard exists to prevent.
func classifyCypherPorts(src *cypherPackageSource, ports map[string]struct{}) []cypherPortClassification {
	if len(src.configurations) > 0 {
		return classifyCypherPortsAcrossConfigurations(src.configurations, ports)
	}
	return classifyCypherPortsInConfiguration(src, ports)
}

func classifyCypherPortsAcrossConfigurations(
	configurations []*cypherPackageSource,
	ports map[string]struct{},
) []cypherPortClassification {
	byPort := map[string]cypherPortClassification{}
	for _, configuration := range configurations {
		for _, row := range classifyCypherPortsInConfiguration(configuration, ports) {
			current, seen := byPort[row.Port]
			if !seen || (!current.WritesEdges && row.WritesEdges) {
				byPort[row.Port] = row
				continue
			}
			if current.WritesEdges {
				continue
			}
			current.UnknownRefs = mergeUnique(current.UnknownRefs, row.UnknownRefs)
			byPort[row.Port] = current
		}
	}
	out := make([]cypherPortClassification, 0, len(byPort))
	for _, row := range byPort {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

func classifyCypherPortsInConfiguration(
	src *cypherPackageSource,
	ports map[string]struct{},
) []cypherPortClassification {
	var out []cypherPortClassification
	for name := range ports {
		keysByPort := src.keysByName
		if src.rootKeysByPort != nil {
			keysByPort = src.rootKeysByPort
		}
		keys, ok := keysByPort[name]
		if !ok {
			continue
		}
		sort.Strings(keys)
		row := cypherPortClassification{Port: name, Impl: src.fileByKey[keys[0]]}
		for _, key := range keys {
			evidence, found, unknownRefs := src.reachesRelationshipMerge(key)
			row.UnknownRefs = mergeUnique(row.UnknownRefs, unknownRefs)
			if found {
				row.WritesEdges = true
				row.Evidence = evidence
				row.UnknownRefs = nil
				break
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// reachesRelationshipMerge walks the call graph from key and returns the first
// relationship-merging Cypher line it finds.
func (s *cypherPackageSource) reachesRelationshipMerge(key string) (string, bool, []string) {
	seen := map[string]struct{}{key: {}}
	queue := []string{key}
	var unknownRefs []string
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, lit := range s.bodyLiterals[current] {
			if line, ok := relationshipMergeLine(lit); ok {
				return line, true, nil
			}
		}
		unknownRefs = mergeUnique(unknownRefs, s.bodyUnknownRefs[current])
		for _, next := range s.bodyCalls[current] {
			if _, exists := s.bodyCalls[next]; !exists {
				continue
			}
			if _, visited := seen[next]; visited {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return "", false, unknownRefs
}
