// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Search index table names, the closed `table` label set of the orphan
// census gauge and the swept-rows counters. The orphan census walks only the
// terms leaves and the documents table; the #8231 superseded sweep also
// deletes each swept generation's single stats row, which its counter
// reports under StatsTable.
const (
	TermsTable     = "eshu_search_index_terms"
	DocumentsTable = "eshu_search_index_documents"
	StatsTable     = "eshu_search_index_stats"
)

// OrphanListLimit is the most orphan pairs one census lists per relation (a
// terms leaf or the documents table) for the sweep. The census still counts
// every orphan; a pass that lists fewer than it counted leaves the rest for
// the next pass.
const OrphanListLimit = 1000

// Census is one orphan census of the search index tables: the
// (scope_id, generation_id) pairs present in a table that scope_generations
// no longer holds.
type Census struct {
	// PairsScanned is the distinct pairs the census walked, by table.
	PairsScanned map[string]int64
	// OrphanGenerations is the orphan pairs it found, by table. Both tables
	// always have an entry, so a clean census reads 0.
	OrphanGenerations map[string]int64
	// Orphans is the listed orphan pairs of both tables, de-duplicated and
	// sorted by scope then generation.
	Orphans []Generation
	// Unlisted is the orphan pairs the census counted but did not list,
	// summed over relations: a relation with more than OrphanListLimit
	// orphans lists only the first OrphanListLimit. The sweep cannot reach
	// them this pass; a later census lists them (#8281 review).
	Unlisted int64
}

// TakeCensus counts the orphan pairs of eshu_search_index_terms (leaf by
// leaf, so each loose index scan stays inside one leaf's primary key) and
// eshu_search_index_documents, one read-only statement per relation, each in
// its own snapshot. It checks ctx between relations. A pair whose
// generation row is written between the walk and the sweep is not deleted:
// every sweep batch re-checks the pair in its own snapshot.
func TakeCensus(ctx context.Context, q db.Queryer) (Census, error) {
	census := Census{
		PairsScanned:      map[string]int64{TermsTable: 0, DocumentsTable: 0},
		OrphanGenerations: map[string]int64{TermsTable: 0, DocumentsTable: 0},
	}
	leaves, err := termsLeaves(ctx, q)
	if err != nil {
		return Census{}, err
	}
	relations := make([][2]string, 0, len(leaves)+1)
	for _, leaf := range leaves {
		relations = append(relations, [2]string{TermsTable, leaf})
	}
	relations = append(relations, [2]string{DocumentsTable, DocumentsTable})
	seen := make(map[Generation]bool)
	for _, relation := range relations {
		if err := ctx.Err(); err != nil {
			return Census{}, fmt.Errorf("search index orphan census: %w", err)
		}
		pairs, orphans, listed, err := censusRelation(ctx, q, relation[1])
		if err != nil {
			return Census{}, err
		}
		census.PairsScanned[relation[0]] += pairs
		census.OrphanGenerations[relation[0]] += orphans
		if unlisted := orphans - int64(len(listed)); unlisted > 0 {
			census.Unlisted += unlisted
		}
		for _, generation := range listed {
			if !seen[generation] {
				seen[generation] = true
				census.Orphans = append(census.Orphans, generation)
			}
		}
	}
	sort.Slice(census.Orphans, func(i, j int) bool {
		if census.Orphans[i].ScopeID != census.Orphans[j].ScopeID {
			return census.Orphans[i].ScopeID < census.Orphans[j].ScopeID
		}
		return census.Orphans[i].GenerationID < census.Orphans[j].GenerationID
	})
	return census, nil
}

// termsLeaves returns the terms leaf partitions, or the table itself on an
// install whose terms table is not partitioned.
func termsLeaves(ctx context.Context, q db.Queryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, termsLeavesQuery)
	if err != nil {
		return nil, fmt.Errorf("search index orphan census: list terms leaves: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var leaves []string
	for rows.Next() {
		var leaf string
		if err := rows.Scan(&leaf); err != nil {
			return nil, fmt.Errorf("search index orphan census: scan terms leaf: %w", err)
		}
		leaves = append(leaves, leaf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search index orphan census: list terms leaves: %w", err)
	}
	if len(leaves) == 0 {
		leaves = []string{TermsTable}
	}
	return leaves, nil
}

// censusRelation runs the census statement over one relation.
func censusRelation(ctx context.Context, q db.Queryer, relation string) (pairs, orphans int64, listed []Generation, err error) {
	rows, err := q.QueryContext(ctx, censusQuery(relation), OrphanListLimit)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("search index orphan census of %s: %w", relation, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scopeID, generationID sql.NullString
		if err := rows.Scan(&pairs, &orphans, &scopeID, &generationID); err != nil {
			return 0, 0, nil, fmt.Errorf("search index orphan census of %s: scan: %w", relation, err)
		}
		if scopeID.Valid && generationID.Valid {
			listed = append(listed, Generation{ScopeID: scopeID.String, GenerationID: generationID.String})
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, nil, fmt.Errorf("search index orphan census of %s: %w", relation, err)
	}
	return pairs, orphans, listed, nil
}
