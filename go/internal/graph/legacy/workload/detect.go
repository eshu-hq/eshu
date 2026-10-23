// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workload

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// Reader is the graph read port Detect needs. querycontract.GraphQuery and
// the reducer's query.GraphQuery satisfy it structurally.
type Reader interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// Options bounds one detection pass.
type Options struct {
	// IDs restricts the pass to these node ids. Empty means page the whole
	// label.
	IDs []string
	// MaxEligible stops the scan once this many eligible candidates were
	// found. Zero means no limit.
	MaxEligible int
	// MaxExamined caps how many candidates the pass reads and evaluates.
	// Zero or negative uses DefaultMaxExamined.
	MaxExamined int
	// PageSize is how many candidates one read pages and evaluates. It is
	// clamped to 1..MaxBatch*5; zero uses MaxBatch.
	PageSize int
}

// DefaultMaxExamined bounds candidates examined when Options.MaxExamined is
// zero.
const DefaultMaxExamined = 1000

// Report is the outcome of one detection pass.
type Report struct {
	// Verdicts holds one verdict per examined candidate, in id order.
	Verdicts []Verdict
	// Truncated is true when the pass stopped before the label was
	// exhausted (the examined cap or MaxEligible), so more candidates may
	// exist.
	Truncated bool
}

// Eligible counts the eligible verdicts.
func (r Report) Eligible() int {
	n := 0
	for _, v := range r.Verdicts {
		if v.Eligible {
			n++
		}
	}
	return n
}

// WithScopedTwin counts the candidates that are not blocked by instances or a
// missing twin: the readiness definition of a pre-#5385 leftover. A candidate
// blocked only by relationships still counts.
func (r Report) WithScopedTwin() int {
	n := 0
	for _, v := range r.Verdicts {
		if v.BlockedReason == BlockedNone || v.BlockedReason == BlockedRelationships {
			n++
		}
	}
	return n
}

// Detect reads the legacy-shaped nodes and everything Evaluate needs about
// them, then evaluates them. Every graph read is a bounded, identity-anchored
// statement; the verdict logic runs in Go so one code path serves the
// readiness check, the dry run, and the pre-delete verification. Those reads
// are offered on Neo4j only, the backend this was tested on.
func Detect(ctx context.Context, reader Reader, opts Options) (Report, error) {
	if reader == nil {
		return Report{}, errors.New("legacy workload detection requires a graph reader")
	}
	maxExamined := opts.MaxExamined
	if maxExamined <= 0 {
		maxExamined = DefaultMaxExamined
	}
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = MaxBatch
	}
	pageSize = min(pageSize, MaxBatch*5)

	var report Report
	if len(opts.IDs) > 0 {
		return detectIDs(ctx, reader, opts.IDs, maxExamined, pageSize)
	}
	after := ""
	for {
		want := min(pageSize, maxExamined-len(report.Verdicts))
		if want <= 0 {
			report.Truncated = true
			return report, nil
		}
		// One extra row tells a full page from an exhausted label without a
		// second read.
		rows, err := reader.Run(ctx, candidatePageCypher, map[string]any{"after": after, "limit": want + 1})
		if err != nil {
			return Report{}, fmt.Errorf("read legacy workload candidates: %w", err)
		}
		more := len(rows) > want
		if more {
			rows = rows[:want]
		}
		page, err := evaluatePage(ctx, reader, candidatesFromRows(rows))
		if err != nil {
			return Report{}, err
		}
		report.Verdicts = append(report.Verdicts, page...)
		if len(rows) > 0 {
			after = stringVal(rows[len(rows)-1], "id")
		}
		if opts.MaxEligible > 0 && report.Eligible() >= opts.MaxEligible {
			report.Truncated = more
			return report, nil
		}
		if !more {
			return report, nil
		}
	}
}

func detectIDs(ctx context.Context, reader Reader, ids []string, maxExamined, pageSize int) (Report, error) {
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	var report Report
	for start := 0; start < len(ids); start += pageSize {
		if len(report.Verdicts) >= maxExamined {
			report.Truncated = true
			return report, nil
		}
		chunk := ids[start:min(start+pageSize, len(ids))]
		rows, err := reader.Run(ctx, candidatesByIDsCypher, map[string]any{"ids": chunk})
		if err != nil {
			return Report{}, fmt.Errorf("read legacy workload candidates by id: %w", err)
		}
		page, err := evaluatePage(ctx, reader, candidatesFromRows(rows))
		if err != nil {
			return Report{}, err
		}
		report.Verdicts = append(report.Verdicts, page...)
	}
	return report, nil
}

// evaluatePage reads the per-candidate facts for one page and evaluates it.
func evaluatePage(ctx context.Context, reader Reader, candidates []Candidate) ([]Verdict, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(candidates))
	twinIDs := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
		if twin := TwinID(c.RepoID, c.Name); twin != "" {
			twinIDs = append(twinIDs, twin)
		}
	}
	facts := Facts{Candidates: candidates}

	edgeRows, err := reader.Run(ctx, instanceEdgeCypher, map[string]any{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("read legacy workload instance edges: %w", err)
	}
	facts.EdgeInstances = idSet(edgeRows)

	propRows, err := reader.Run(ctx, instancePropertyCypher, map[string]any{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("read legacy workload instance references: %w", err)
	}
	facts.PropInstances = idSet(propRows)

	facts.Twins = map[string][]string{}
	if len(twinIDs) > 0 {
		twinRows, err := reader.Run(ctx, twinCypher, map[string]any{"twin_ids": twinIDs})
		if err != nil {
			return nil, fmt.Errorf("read legacy workload twins: %w", err)
		}
		for _, row := range twinRows {
			id := stringVal(row, "id")
			facts.Twins[id] = append(facts.Twins[id], stringVal(row, "repo_id"))
		}
	}

	relRows, err := reader.Run(ctx, relationshipsCypher, map[string]any{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("read legacy workload relationships: %w", err)
	}
	facts.Relationships = map[string][]Relationship{}
	for _, row := range relRows {
		id := stringVal(row, "id")
		outgoing, _ := row["outgoing"].(bool)
		facts.Relationships[id] = append(facts.Relationships[id], Relationship{
			Type:        stringVal(row, "type"),
			Outgoing:    outgoing,
			OtherID:     stringVal(row, "other_id"),
			OtherLabels: stringSlice(row["other_labels"]),
		})
	}
	return Evaluate(facts), nil
}

func candidatesFromRows(rows []map[string]any) []Candidate {
	out := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		id := stringVal(row, "id")
		name := stringVal(row, "name")
		// The read already filtered on the legacy shape; re-checking it here
		// keeps a mocked or drifting backend from widening the candidate set.
		if !IsLegacyID(id, name) {
			continue
		}
		out = append(out, Candidate{ID: id, Name: name, RepoID: stringVal(row, "repo_id")})
	}
	return out
}

func idSet(rows []map[string]any) map[string]bool {
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		if id := stringVal(row, "id"); id != "" {
			set[id] = true
		}
	}
	return set
}

func stringVal(row map[string]any, key string) string {
	s, _ := row[key].(string)
	return s
}

func stringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
