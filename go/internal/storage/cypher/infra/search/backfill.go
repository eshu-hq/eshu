// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultBackfillBatchSize is the number of nodes the backfill commits per
// inner transaction when BackfillOptions.BatchSize is not positive.
const DefaultBackfillBatchSize = 1000

// Runner runs one Cypher statement in its own auto-commit transaction and
// returns its rows. The backfill needs auto-commit: CALL { ... } IN
// TRANSACTIONS is rejected inside an explicit transaction.
type Runner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// BackfillOptions configures Backfill.
type BackfillOptions struct {
	// BatchSize is the nodes per inner transaction; <= 0 uses
	// DefaultBackfillBatchSize.
	BatchSize int
	// Logger receives one event per label and one summary event. Nil
	// disables logging.
	Logger *slog.Logger
}

// LabelBackfill is the outcome of the backfill for one label.
type LabelBackfill struct {
	// Label is the infra search label.
	Label string
	// Keyed is the number of nodes whose key or overflow flag was written.
	Keyed int64
	// Overflowed is how many of those nodes got no key because their joined
	// fields are longer than graph.InfraSearchKeyMaxChars.
	Overflowed int64
	// Duration is the wall time of the label's statement.
	Duration time.Duration
}

// Backfill writes the search key on every infra search node whose stored key
// or overflow flag disagrees with its current fields, label by label, in
// batches of opts.BatchSize nodes per transaction. It is idempotent: a node
// already keyed from its current fields is not rewritten, so a second run
// writes nothing. It also repairs a key a writer without the clause left
// stale. It writes only infra_search_key and infra_search_ovf, through
// the same Clause every writer uses.
//
// Neo4j only: the clause does not evaluate on NornicDB. It stops at the first
// failed label and returns the label in the error; the caller retries the
// whole run.
func Backfill(ctx context.Context, runner Runner, opts BackfillOptions) ([]LabelBackfill, error) {
	batch := opts.BatchSize
	if batch <= 0 {
		batch = DefaultBackfillBatchSize
	}
	start := time.Now()
	labels := graph.InfraSearchKeyLabels()
	results := make([]LabelBackfill, 0, len(labels))
	var keyed, overflowed int64
	for _, label := range labels {
		labelStart := time.Now()
		rows, err := runner.Run(ctx, backfillCypher(label), map[string]any{"batch_size": batch})
		if err != nil {
			return results, fmt.Errorf("infra search key backfill for label %s: %w", label, err)
		}
		result := LabelBackfill{
			Label:      label,
			Keyed:      firstInt(rows, "keyed"),
			Overflowed: firstInt(rows, "overflowed"),
			Duration:   time.Since(labelStart),
		}
		results = append(results, result)
		keyed += result.Keyed
		overflowed += result.Overflowed
		if opts.Logger != nil {
			opts.Logger.InfoContext(ctx, "infra search key backfilled for label",
				telemetry.EventAttr("bootstrap.graph.infra_search_key.label_backfilled"),
				"label", label,
				"nodes_keyed", result.Keyed,
				"overflow_count", result.Overflowed,
				"batch_size", batch,
				"duration_seconds", result.Duration.Seconds(),
			)
		}
	}
	if opts.Logger != nil {
		opts.Logger.InfoContext(ctx, "infra search key backfill completed",
			telemetry.EventAttr("bootstrap.graph.infra_search_key.backfilled"),
			"label_count", len(labels),
			"nodes_keyed", keyed,
			"overflow_count", overflowed,
			"duration_seconds", time.Since(start).Seconds(),
		)
	}
	return results, nil
}

// OversizedKeyCount returns how many infra search nodes hold a key longer
// than graph.InfraSearchKeyMaxChars. The writer clause never stores one, so
// a non-zero count means something bypassed it. Such a key is one FULLTEXT
// term over Lucene's term limit: creating or populating the index over it
// fails the index. Bootstrap runs this before the index DDL and refuses to
// continue when it is not zero. Neo4j only.
func OversizedKeyCount(ctx context.Context, runner Runner) (int64, error) {
	rows, err := runner.Run(ctx, OversizedKeyCountCypher(), nil)
	if err != nil {
		return 0, fmt.Errorf("count oversized infra search keys: %w", err)
	}
	return firstInt(rows, "oversized"), nil
}

// OversizedKeyCountCypher is the readiness probe statement OversizedKeyCount
// runs, exported for the FAILED-index runbook.
func OversizedKeyCountCypher() string {
	return "MATCH (n:" + strings.Join(graph.InfraSearchKeyLabels(), "|") + ")\n" +
		"WHERE size(n." + graph.InfraSearchKeyProperty + ") > " + strconv.Itoa(graph.InfraSearchKeyMaxChars) + "\n" +
		"RETURN count(n) AS oversized"
}

// CountUnkeyed returns, per infra search label, the nodes that carry no
// overflow flag: nodes no keying writer or backfill has touched. The read
// cutover (#7245 PR-2) may trust the key only when every count is zero and
// the backfill has repaired stale keys. Neo4j only.
func CountUnkeyed(ctx context.Context, runner Runner) (map[string]int64, error) {
	labels := graph.InfraSearchKeyLabels()
	counts := make(map[string]int64, len(labels))
	for _, label := range labels {
		rows, err := runner.Run(ctx, "MATCH (n:"+label+")\nWHERE n."+graph.InfraSearchOverflowProperty+
			" IS NULL\nRETURN count(n) AS unkeyed", nil)
		if err != nil {
			return nil, fmt.Errorf("count unkeyed infra search nodes for label %s: %w", label, err)
		}
		counts[label] = firstInt(rows, "unkeyed")
	}
	return counts, nil
}

// backfillCypher builds the per-label backfill. It computes the joined key
// once per node, selects the nodes whose stored flag or key disagrees with
// it, and writes them with Clause in batched inner transactions. The overflow
// count is read from the joined key, not the written flag, so it does not
// depend on read-after-write inside the outer query.
func backfillCypher(label string) string {
	const joined = "infra_search_current_key"
	bound := strconv.Itoa(graph.InfraSearchKeyMaxChars)
	return "MATCH (n:" + label + ")\n" +
		"WITH n, " + joinedExpr("n") + " AS " + joined + "\n" +
		"WHERE n." + graph.InfraSearchOverflowProperty + " IS NULL\n" +
		"   OR n." + graph.InfraSearchOverflowProperty + " <> " + overflowFlagExpr(joined) + "\n" +
		"   OR coalesce(n." + graph.InfraSearchKeyProperty + ", '') <> coalesce(" + keyValueExpr(joined) + ", '')\n" +
		"CALL (n) {\n  " + Clause("n") + "\n} IN TRANSACTIONS OF $batch_size ROWS\n" +
		"RETURN count(n) AS keyed, sum(CASE WHEN size(" + joined + ") > " + bound + " THEN 1 ELSE 0 END) AS overflowed"
}

// firstInt reads an integer column from the first row, or 0.
func firstInt(rows []map[string]any, column string) int64 {
	if len(rows) == 0 {
		return 0
	}
	switch v := rows[0][column].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}
