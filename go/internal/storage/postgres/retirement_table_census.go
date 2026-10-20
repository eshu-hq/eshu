// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/retirement"
)

// retirementCensusParents are the five parent tables whose foreign keys
// (or key columns) make a table retirement-relevant.
var retirementCensusParents = []string{
	"ingestion_scopes",
	"scope_generations",
	"fact_records",
	"fact_work_items",
	"content_files",
}

// RetirementCensusHit is one information_schema hit the closed census
// table must classify: a table in the live schema that either holds a
// foreign key to a retirement parent or carries a scope, repository, or
// partition-key column.
type RetirementCensusHit struct {
	// Table is the parent table name (partitions roll up).
	Table string
	// Columns are the matching key columns that made it a hit.
	Columns []string
	// ViaForeignKey is true when a foreign key to a retirement parent
	// (not just the column name) makes it a hit.
	ViaForeignKey bool
}

// retirementCensusColumnHits lists, per table, the scope/repository/
// partition-key columns in the live schema. It reads pg_catalog, not
// information_schema, so views (which hold no rows) never report while
// ordinary tables, partitioned tables, and materialized views do.
const retirementCensusColumnHitsSQL = `SELECT cl.relname, array_agg(DISTINCT a.attname ORDER BY a.attname)
	FROM pg_attribute a
	JOIN pg_class cl ON cl.oid = a.attrelid
	JOIN pg_namespace n ON n.oid = cl.relnamespace
	WHERE n.nspname = $1 AND cl.relkind IN ('r', 'p', 'm')
	  AND a.attnum > 0 AND NOT a.attisdropped
	  AND (a.attname LIKE '%repo%' OR a.attname LIKE '%scope%' OR a.attname = 'partition_key')
	GROUP BY cl.relname`

// retirementCensusForeignKeyHits lists the tables holding a foreign key
// to one of the five retirement parents.
const retirementCensusForeignKeyHitsSQL = `SELECT DISTINCT cl.relname
	FROM pg_constraint c
	JOIN pg_class cl ON cl.oid = c.conrelid
	JOIN pg_namespace n ON n.oid = cl.relnamespace
	JOIN pg_class t ON t.oid = c.confrelid
	WHERE c.contype = 'f' AND n.nspname = $1 AND t.relname = ANY($2)`

// retirementCensusPartitionParents maps each partitioned child table to
// its parent so the hits roll up (the closed table classifies parents).
const retirementCensusPartitionParentsSQL = `SELECT cl.relname, p.relname
	FROM pg_class cl
	JOIN pg_namespace n ON n.oid = cl.relnamespace
	JOIN pg_inherits i ON i.inhrelid = cl.oid
	JOIN pg_class p ON p.oid = i.inhparent
	WHERE n.nspname = $1 AND cl.relispartition`

// RetirementCensusUnclassified scans the live schema the database
// connection sees (its search_path schema) and returns every census hit
// missing from the closed retirement table, plus every closed-table row
// the scan can no longer see without an invisibility reason. An empty
// result means the census covers the schema.
func RetirementCensusUnclassified(ctx context.Context, database *sql.DB) ([]RetirementCensusHit, []string, error) {
	var schema string
	if err := database.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return nil, nil, fmt.Errorf("retirement census current schema: %w", err)
	}
	columnRows, err := database.QueryContext(ctx, retirementCensusColumnHitsSQL, schema)
	if err != nil {
		return nil, nil, fmt.Errorf("retirement census column hits: %w", err)
	}
	defer func() { _ = columnRows.Close() }()
	hits := make(map[string]*RetirementCensusHit)
	for columnRows.Next() {
		var table, columns string
		if err := columnRows.Scan(&table, &columns); err != nil {
			return nil, nil, fmt.Errorf("retirement census column hit scan: %w", err)
		}
		hits[table] = &RetirementCensusHit{Table: table, Columns: parseCensusColumns(columns)}
	}
	if err := columnRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("retirement census column hits: %w", err)
	}
	fkRows, err := database.QueryContext(ctx, retirementCensusForeignKeyHitsSQL, schema, retirementCensusParents)
	if err != nil {
		return nil, nil, fmt.Errorf("retirement census foreign key hits: %w", err)
	}
	defer func() { _ = fkRows.Close() }()
	for fkRows.Next() {
		var table string
		if err := fkRows.Scan(&table); err != nil {
			return nil, nil, fmt.Errorf("retirement census foreign key hit scan: %w", err)
		}
		hit, ok := hits[table]
		if !ok {
			hit = &RetirementCensusHit{Table: table}
			hits[table] = hit
		}
		hit.ViaForeignKey = true
	}
	if err := fkRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("retirement census foreign key hits: %w", err)
	}
	partRows, err := database.QueryContext(ctx, retirementCensusPartitionParentsSQL, schema)
	if err != nil {
		return nil, nil, fmt.Errorf("retirement census partition parents: %w", err)
	}
	defer func() { _ = partRows.Close() }()
	parents := make(map[string]string)
	for partRows.Next() {
		var child, parent string
		if err := partRows.Scan(&child, &parent); err != nil {
			return nil, nil, fmt.Errorf("retirement census partition parent scan: %w", err)
		}
		parents[child] = parent
	}
	if err := partRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("retirement census partition parents: %w", err)
	}
	rolled := make(map[string]*RetirementCensusHit)
	for table, hit := range hits {
		if parent, ok := parents[table]; ok {
			table = parent
		}
		merged, ok := rolled[table]
		if !ok {
			merged = &RetirementCensusHit{Table: table}
			rolled[table] = merged
		}
		merged.Columns = mergeCensusColumns(merged.Columns, hit.Columns)
		merged.ViaForeignKey = merged.ViaForeignKey || hit.ViaForeignKey
	}
	var unclassified []RetirementCensusHit
	for _, hit := range rolled {
		if _, ok := retirement.RetireTableDispositionFor(hit.Table); !ok {
			unclassified = append(unclassified, *hit)
		}
	}
	sort.Slice(unclassified, func(i, j int) bool { return unclassified[i].Table < unclassified[j].Table })
	var stale []string
	for table, entry := range retirement.RetireTableDispositions {
		if _, ok := rolled[table]; ok {
			continue
		}
		if strings.Contains(entry.Reason, "the schema scan cannot see it") {
			continue
		}
		stale = append(stale, table)
	}
	sort.Strings(stale)
	return unclassified, stale, nil
}

// parseCensusColumns splits a Postgres text-array aggregate such as
// {repo_id,scope_id} into column names.
func parseCensusColumns(aggregate string) []string {
	aggregate = strings.Trim(aggregate, "{}")
	if aggregate == "" {
		return nil
	}
	return strings.Split(aggregate, ",")
}

// mergeCensusColumns unions two sorted column lists without duplicates.
func mergeCensusColumns(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]string, 0, len(left)+len(right))
	for _, column := range append(append([]string{}, left...), right...) {
		if _, ok := seen[column]; ok {
			continue
		}
		seen[column] = struct{}{}
		merged = append(merged, column)
	}
	sort.Strings(merged)
	return merged
}
