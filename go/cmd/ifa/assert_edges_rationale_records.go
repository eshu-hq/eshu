// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa/graphdump"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	"github.com/eshu-hq/eshu/go/internal/replay"
)

// assertRationaleMaterializedEdgeRecords compares the complete live EXPLAINS
// records scoped to repoID with the hand-derived fixture as a multiset. An edge
// is in scope when either endpoint belongs to the repository or carries an
// expected endpoint identity. The identity fallback keeps repository-property
// drift visible instead of silently reclassifying the expected edge as foreign.
func assertRationaleMaterializedEdgeRecords(
	ctx context.Context,
	reader graphdump.Reader,
	repoID string,
	expected []materializededges.RationaleExpectedEdgeRecord,
) error {
	expectedCounts := make(map[string]int, len(expected))
	expectedEndpointIDs := make(map[string]struct{}, 2*len(expected))
	for i, record := range expected {
		key, err := canonicalRationaleExpectedRecord(record)
		if err != nil {
			return fmt.Errorf("ifa assert-edges: canonicalize expected rationale record %d: %w", i, err)
		}
		expectedCounts[key]++
		expectedEndpointIDs[record.SourceEntityID] = struct{}{}
		expectedEndpointIDs[record.TargetEntityID] = struct{}{}
	}

	actualCounts := make(map[string]int, len(expected))
	err := reader.StreamEdges(ctx, func(edge graphdump.Edge) error {
		if edge.Type != "EXPLAINS" || !rationaleEdgeIsInScope(edge, repoID, expectedEndpointIDs) {
			return nil
		}
		key, err := canonicalRationaleGraphEdge(edge)
		if err != nil {
			return fmt.Errorf("canonicalize live rationale record: %w", err)
		}
		actualCounts[key]++
		return nil
	})
	if err != nil {
		return fmt.Errorf("ifa assert-edges: stream rationale_edges records: %w", err)
	}

	missing, extra, duplicate := rationaleRecordMultisetDiff(expectedCounts, actualCounts)
	if len(missing) == 0 && len(extra) == 0 && len(duplicate) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ifa assert-edges: domain=rationale_edges full materialized edge records do not match the expected multiset exactly")
	appendRationaleRecordDiff(&b, "missing", missing)
	appendRationaleRecordDiff(&b, "extra", extra)
	appendRationaleRecordDiff(&b, "duplicate", duplicate)
	return fmt.Errorf("%s", b.String())
}

func rationaleEdgeIsInScope(
	edge graphdump.Edge,
	repoID string,
	expectedEndpointIDs map[string]struct{},
) bool {
	if rationaleEdgeTouchesRepository(edge, repoID) {
		return true
	}
	if _, ok := expectedEndpointIDs[endpointID(edge.FromProps, edge.FromLabels)]; ok {
		return true
	}
	_, ok := expectedEndpointIDs[endpointID(edge.ToProps, edge.ToLabels)]
	return ok
}

func rationaleEdgeTouchesRepository(edge graphdump.Edge, repoID string) bool {
	fromRepo, _ := edge.FromProps["repo_id"].(string)
	toRepo, _ := edge.ToProps["repo_id"].(string)
	return fromRepo == repoID || toRepo == repoID
}

func canonicalRationaleExpectedRecord(record materializededges.RationaleExpectedEdgeRecord) (string, error) {
	return canonicalRationaleRecord(
		record.RelationshipType,
		record.SourceRecord.Labels,
		record.SourceRecord.Props,
		record.EdgeProps,
		record.TargetRecord.Labels,
		record.TargetRecord.Props,
	)
}

func canonicalRationaleGraphEdge(edge graphdump.Edge) (string, error) {
	return canonicalRationaleRecord(edge.Type, edge.FromLabels, edge.FromProps, edge.Props, edge.ToLabels, edge.ToProps)
}

func canonicalRationaleRecord(
	edgeType string,
	fromLabels []string,
	fromProps map[string]any,
	edgeProps map[string]any,
	toLabels []string,
	toProps map[string]any,
) (string, error) {
	fromLabels = append([]string(nil), fromLabels...)
	toLabels = append([]string(nil), toLabels...)
	sort.Strings(fromLabels)
	sort.Strings(toLabels)
	record := map[string]any{
		"type":   edgeType,
		"source": map[string]any{"labels": fromLabels, "props": fromProps},
		"props":  edgeProps,
		"target": map[string]any{"labels": toLabels, "props": toProps},
	}
	canonical, err := replay.CanonicalizeValue(record, replay.CanonicalOptions{})
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func rationaleRecordMultisetDiff(expected, actual map[string]int) (missing, extra, duplicate []string) {
	for key, want := range expected {
		got := actual[key]
		if got < want {
			missing = append(missing, fmt.Sprintf("%s (graph=%d, expected=%d)", compactCanonicalRecord(key), got, want))
		}
		if got > want {
			duplicate = append(duplicate, fmt.Sprintf("%s (graph=%d, expected=%d)", compactCanonicalRecord(key), got, want))
		}
	}
	for key, got := range actual {
		if _, ok := expected[key]; !ok {
			extra = append(extra, fmt.Sprintf("%s (graph=%d, expected=0)", compactCanonicalRecord(key), got))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(duplicate)
	return missing, extra, duplicate
}

func compactCanonicalRecord(record string) string {
	return strings.Join(strings.Fields(record), " ")
}

func appendRationaleRecordDiff(b *strings.Builder, label string, records []string) {
	if len(records) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  %s (%d):", label, len(records))
	for _, record := range records {
		fmt.Fprintf(b, "\n    %s", record)
	}
}
