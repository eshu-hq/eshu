// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package completionstore

import (
	"regexp"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

var (
	forClausePattern = regexp.MustCompile(`(?i)\bfor\s+(no\s+key\s+update|update|share|key\s+share)\b`)
	dmlPattern       = regexp.MustCompile(`(?i)\b(insert|update|delete)\b`)
)

// manifestMarkerFanoutShapeError reports how query fails the #10972 shape of the
// fan-out statement: the locked, scheduled current_consumers set excludes
// manifest markers, and the statement makes no second fact_work_items pass over
// the marker rows and returns only the four fan-out counts (the marker count is
// a separate statement, see manifestMarkerCountShapeError). It exists as a
// function so the seeded-violation tests can feed it a broken statement.
func manifestMarkerFanoutShapeError(query string) string {
	consumers := between(query, "current_consumers AS MATERIALIZED (", "), captured AS MATERIALIZED (")
	switch {
	case consumers == "":
		return "current_consumers CTE is missing"
	case !strings.Contains(consumers, "AND NOT ("+manifestMarkerPredicate+")"):
		return "current_consumers does not exclude manifest markers"
	case strings.Contains(query, "skipped_markers") || strings.Contains(query, "AND ("+manifestMarkerPredicate+")"):
		return "fan-out statement must not read the marker rows a second time"
	case strings.Count(query, "FROM fact_work_items AS source") != 1:
		return "fan-out statement must make exactly one fact_work_items pass (current_consumers)"
	case strings.Count(query[strings.LastIndex(query, "SELECT (SELECT count(*) FROM captured)"):], "(SELECT ") != 4:
		return "fan-out statement must return exactly four counts"
	case !strings.Contains(query, activeGenerationsCTE) ||
		!strings.Contains(query, activeGenerationJoin) ||
		!strings.Contains(query, consumerStatePredicate):
		return "fan-out statement does not use the shared active-generation and state text"
	case !strings.Contains(manifestMarkerPredicate, "payload->>'trigger'") ||
		!strings.Contains(manifestMarkerPredicate, "'manifest_dependency'") ||
		!strings.Contains(manifestMarkerPredicate, "source.domain = 'supply_chain_impact'"):
		return "marker predicate lost its trigger, value, or domain guard"
	}
	return ""
}

// manifestMarkerCountShapeError reports how query fails the #10972 shape of the
// post-commit marker count: a plain read of the marker rows built from the
// same predicate and active-generation text as the fan-out, with no FOR clause
// and no DML.
func manifestMarkerCountShapeError(query string) string {
	switch {
	case !strings.Contains(query, "AND ("+manifestMarkerPredicate+")"):
		return "count does not read the marker predicate positively"
	case forClausePattern.MatchString(query):
		return "count must not carry a FOR clause"
	case dmlPattern.MatchString(query):
		return "count must not carry DML"
	case !strings.Contains(query, activeGenerationsCTE) ||
		!strings.Contains(query, activeGenerationJoin) ||
		!strings.Contains(query, consumerStatePredicate):
		return "count does not use the shared active-generation and state text"
	}
	return ""
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	rest := s[i+len(from):]
	j := strings.Index(rest, to)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestFanoutQueryExcludesManifestMarkers pins that the shipped completion
// fan-out leaves manifest markers unlocked and unscheduled (#10972) without a
// second pass over them; the live twin proves the rows. Each seeded violation
// must fail the guard.
func TestFanoutQueryExcludesManifestMarkers(t *testing.T) {
	t.Parallel()

	if msg := manifestMarkerFanoutShapeError(FanoutCrossScopeCompletionQuery); msg != "" {
		t.Fatalf("shipped fan-out query: %s", msg)
	}
	exclusion := "AND NOT (" + manifestMarkerPredicate + ")"
	for name, broken := range map[string]string{
		"exclusion dropped": strings.Replace(FanoutCrossScopeCompletionQuery, exclusion, "", 1),
		"skipped_markers CTE re-added": strings.Replace(FanoutCrossScopeCompletionQuery,
			"), captured AS MATERIALIZED (",
			"), skipped_markers AS MATERIALIZED (\n    SELECT count(*) AS skipped\n    FROM fact_work_items AS source\n    WHERE (1=1)\n), captured AS MATERIALIZED (", 1),
		"positive marker read re-added": strings.Replace(FanoutCrossScopeCompletionQuery,
			"ORDER BY source.work_item_id COLLATE \"C\"",
			"AND ("+manifestMarkerPredicate+")\n    ORDER BY source.work_item_id COLLATE \"C\"", 1),
		"second read under another alias": strings.Replace(FanoutCrossScopeCompletionQuery,
			"), captured AS MATERIALIZED (",
			"), marker_census AS MATERIALIZED (\n    SELECT count(*) AS n\n    FROM fact_work_items AS m\n    WHERE m.domain = 'supply_chain_impact'\n), captured AS MATERIALIZED (", 1),
		"second read through a join": strings.Replace(FanoutCrossScopeCompletionQuery,
			"FROM fact_work_items AS source",
			"FROM fact_work_items AS source\n    JOIN fact_work_items x ON x.scope_id = source.scope_id", 1),
		"second read in a subselect": strings.Replace(FanoutCrossScopeCompletionQuery,
			"AND (SELECT count(*) FROM current_consumers) >= 0",
			"AND (SELECT count(*) FROM fact_work_items AS m WHERE m.domain = 'x') >= 0", 1),
		"reworded positive trigger comparison": strings.Replace(FanoutCrossScopeCompletionQuery,
			exclusion, exclusion+"\n      AND source.payload->>'"+supplychaincore.PayloadKeyTrigger+"' = '"+supplychaincore.TriggerManifestDependency+"'", 1),
		"fifth result column re-added": strings.Replace(FanoutCrossScopeCompletionQuery,
			"(SELECT count(*) FROM deleted)", "(SELECT count(*) FROM deleted),\n       (SELECT 0)", 1),
		"private active-generation copy": strings.Replace(FanoutCrossScopeCompletionQuery,
			activeGenerationJoin, strings.Replace(activeGenerationJoin, "generation.generation_id = source", "generation.generation_id IS NOT NULL AND true OR source", 1), 1),
	} {
		if manifestMarkerFanoutShapeError(broken) == "" {
			t.Errorf("seeded violation %q passed the fan-out shape check", name)
		}
	}
}

// TestManifestMarkerCountQueryIsAPlainRead pins that the post-commit count
// takes no row lock, writes nothing, and shares the fan-out's predicate and
// active-generation text. Each seeded violation must fail the guard.
func TestManifestMarkerCountQueryIsAPlainRead(t *testing.T) {
	t.Parallel()

	if msg := manifestMarkerCountShapeError(manifestMarkerCountQuery); msg != "" {
		t.Fatalf("shipped marker count query: %s", msg)
	}
	for name, broken := range map[string]string{
		"FOR UPDATE":         manifestMarkerCountQuery + "FOR UPDATE OF source",
		"FOR NO KEY UPDATE":  manifestMarkerCountQuery + "FOR NO KEY UPDATE OF source",
		"FOR SHARE":          manifestMarkerCountQuery + "FOR SHARE",
		"DML":                "WITH touched AS (UPDATE fact_work_items SET status = status RETURNING 1)" + manifestMarkerCountQuery,
		"negated predicate":  strings.Replace(manifestMarkerCountQuery, "AND ("+manifestMarkerPredicate+")", "AND NOT ("+manifestMarkerPredicate+")", 1),
		"private state text": strings.Replace(manifestMarkerCountQuery, consumerStatePredicate, "source.stage = 'reducer'", 1),
		"private generation": strings.Replace(manifestMarkerCountQuery, activeGenerationJoin, "JOIN scope_generations AS generation ON generation.scope_id = source.scope_id", 1),
	} {
		if manifestMarkerCountShapeError(broken) == "" {
			t.Errorf("seeded violation %q passed the count shape check", name)
		}
	}
}
