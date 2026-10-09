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
	// Findings lists every uncovered id write, one per statement, variable,
	// and kind.
	Findings []Finding
}

// CheckWriters checks that every statement writing a node id names at least
// one label in anchorLabels, so the labeled entity-context anchor can reach
// the node. Statements repeat across a replay, so identical text, parameters,
// and callsite count once.
//
// For the shapes its tests cover, the check fails closed. A variable the
// statement does not label, a variable rebound to another label, a parameter
// property map, a SET target that is not a plain variable, an id key in a pattern
// the scan cannot place, or a dynamic `SET n += <map>` on an uncovered label
// whose bound parameters do not prove the map has no id key, is a finding. A
// relationship variable is not a node and is skipped. The check is not a full
// Cypher parser; the package README lists the shapes it does not model.
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
