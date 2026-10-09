// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"sort"
)

// Statement is one executed graph statement as the differential capture
// records it: normalized Cypher text plus its JSON-encoded bound parameters.
type Statement struct {
	// Text is the statement's Cypher text.
	Text string
	// Parameters is the JSON object of bound parameters ("" or "{}" for none).
	Parameters string
	// Callsite is the builder identity that produced the statement, when the
	// recording carries one.
	Callsite string
}

// Finding kinds name how a statement writes a node id.
const (
	// KindMapKey is an id key in a MERGE or CREATE node property map.
	KindMapKey = "map_key"
	// KindSetProperty is a `SET n.id = ...` assignment.
	KindSetProperty = "set_property"
	// KindLabelRemoval is a `REMOVE n:Label` of an anchor label that leaves the
	// node with no anchor label in the same statement.
	KindLabelRemoval = "label_removal"
	// KindDynamicMap is a `SET n += <map>` or `SET n = <map>` whose map may
	// carry an id key that the statement text does not settle.
	KindDynamicMap = "dynamic_map"
)

// Finding names one id write on a node that no anchor label covers.
type Finding struct {
	// Variable is the Cypher variable the id was written on.
	Variable string
	// Labels are the labels the statement gives that variable, sorted.
	Labels []string
	// Kind is KindMapKey, KindSetProperty, or KindDynamicMap.
	Kind string
	// Statement is the offending statement text.
	Statement string
	// Callsite is the builder identity, when known.
	Callsite string
}

// Report is the result of checking a set of statements.
type Report struct {
	// Statements is the number of distinct statements examined.
	Statements int
	// IDWrites is the number of distinct statement and variable pairs that
	// write, or may write, a node id.
	IDWrites int
	// Findings lists the uncovered id writes found, one per statement, variable,
	// and kind.
	Findings []Finding
}

// CheckWriters is a heuristic pre-filter over Cypher text. It looks for a
// statement that writes a node id on a node whose labels include no label in
// anchorLabels, the labels the entity-context anchor reaches a node through.
// Statements repeat across a replay, so identical text, parameters, and
// callsite count once.
//
// It reports the shapes its test rows cover; the package README names them
// beside the test functions, and lists the known blind spots (not exhaustive).
// It is not a full Cypher parser. The authority that no unanchored id-bearing
// node ships is the census (CensusCypher), run as the required
// graph/anchor_census check after the replay and as the reducer gauge.
func CheckWriters(statements []Statement, anchorLabels map[string]bool) Report {
	var report Report
	seen := make(map[Statement]bool, len(statements))
	for _, statement := range statements {
		if seen[statement] {
			continue
		}
		seen[statement] = true
		report.Statements++
		writes, findings := analyze(statement, anchorLabels)
		report.IDWrites += writes
		report.Findings = append(report.Findings, findings...)
	}
	return report
}

// analyze returns the id-write count and uncovered findings of one statement.
func analyze(statement Statement, anchorLabels map[string]bool) (int, []Finding) {
	parsed := parseStatement(statement.Text)
	var findings []Finding
	writes := 0
	counted := make(map[string]bool)
	for _, write := range parsed.writes {
		if parsed.nearestIsRelationship(write) {
			continue
		}
		labels := parsed.labelsOf(write)
		covered := false
		for _, label := range labels {
			if anchorLabels[label] {
				covered = true
				break
			}
		}
		key := write.variable + "\x00" + write.kind
		if write.kind == KindDynamicMap && !covered {
			if write.keyExpression != "" {
				if parameterKeyIsNotID(write.keyExpression, statement.Parameters) {
					continue
				}
			} else if parametersProveNoID(write.expression, parsed.unwindAliases, statement.Parameters) {
				continue
			}
		}
		if !counted[key] {
			counted[key] = true
			writes++
		}
		if covered {
			continue
		}
		findings = append(findings, Finding{
			Variable:  write.variable,
			Labels:    labels,
			Kind:      write.kind,
			Statement: statement.Text,
			Callsite:  statement.Callsite,
		})
	}
	removalFound := removalFindings(parsed, statement, anchorLabels)
	writes += len(removalFound)
	findings = append(findings, removalFound...)
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Variable < findings[j].Variable })
	return writes, findings
}
