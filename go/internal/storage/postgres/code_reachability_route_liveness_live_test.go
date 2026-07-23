// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// seedRouteLivenessController seeds one Ruby controller class (ancestry:
// ApplicationController, so #5376 always confirms) plus one action method
// content_entities root, keyed by (repoID, className, actionName). Callers
// build the surrounding repo/scope/generation/acceptance rows themselves so
// each #5494 scenario controls its own route-fact rows.
func seedRouteLivenessController(t *testing.T, ctx context.Context, exec func(string, ...any), repoID, className, actionName string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	exec(`INSERT INTO content_entities
	  (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata, indexed_at)
	  VALUES ($1,$2,'app/controllers/x.rb','Class',$3,1,3,'ruby','', $4::jsonb, $5)`,
		repoID+":class:"+className, repoID, className,
		fmt.Sprintf(`{"qualified_name":"%s","qualified_bases":["ApplicationController"]}`, className), now)
	exec(`INSERT INTO content_entities
	  (entity_id, repo_id, relative_path, entity_type, entity_name, start_line, end_line, language, source_cache, metadata, indexed_at)
	  VALUES ($1,$2,'app/controllers/x.rb','Function',$3,10,15,'ruby','', $4::jsonb, $5)`,
		repoID+":fn:"+className+":"+actionName, repoID, actionName,
		fmt.Sprintf(`{"dead_code_root_kinds":["ruby.rails_controller_action"],"class_context":"%s"}`, className), now)
	_ = ctx
}

// seedRouteLivenessRailsFile seeds one config/routes.rb fact_records row for
// repoID carrying the given exact route_entries handlers (may be empty) and
// hasUnmodeledRoutes flag -- the SAME shape the Ruby parser's
// framework_semantics emits (internal/parser/ruby/framework_routes.go).
func seedRouteLivenessRailsFile(t *testing.T, ctx context.Context, exec func(string, ...any), scopeID, generationID, repoID string, handlers []string, hasUnmodeledRoutes bool) {
	t.Helper()
	entries := ""
	for i, h := range handlers {
		if i > 0 {
			entries += ","
		}
		entries += fmt.Sprintf(`{"method":"GET","path":"/x%d","handler":"%s"}`, i, h)
	}
	payload := fmt.Sprintf(
		`{"repo_id":"%s","relative_path":"config/routes.rb","parsed_file_data":{"framework_semantics":{"frameworks":["rails"],"rails":{"route_entries":[%s],"has_unmodeled_routes":%t}}}}`,
		repoID, entries, hasUnmodeledRoutes,
	)
	now := time.Now().UTC()
	exec(`INSERT INTO fact_records
	  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
	   observed_at, ingested_at, payload)
	  VALUES ($1,$2,$3,'file',$1,'git',$1,$4,$4,$5::jsonb)`,
		"fact-routes-"+repoID, scopeID, generationID, now, payload)
	_ = ctx
}

// TestCodeReachabilityRailsRouteFactsLoaderRoundTrip is the #5494 live,
// real-Postgres proof: an ancestry-confirmed Rails controller action with NO
// backing route downgrades dead when the repo's route surface is exact-only
// and observed; a routed action, an ambiguous (dynamic-route) repo, and a
// repo with no observed route data all stay confirmed. It exercises the ACTUAL
// production path: loadCodeReachabilityRailsRouteFacts (the SQL query proven
// with EXPLAIN in the #5494 proof note) feeding the real
// reducer.BuildCodeRootVerdicts, not a hand-built fixture.
func TestCodeReachabilityRailsRouteFactsLoaderRoundTrip(t *testing.T) {
	ctx, db := openUpgradeBackfillLiveDB(t)
	store := NewCodeReachabilityStore(SQLDB{DB: db})

	suffix := testSuffix(t)
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	repoRouted := "repo-routed-" + suffix
	repoUnrouted := "repo-unrouted-" + suffix
	repoAmbiguous := "repo-ambiguous-" + suffix
	repoNoData := "repo-nodata-" + suffix

	registerUpgradeBackfillCleanup(t, db, scopeID, repoRouted)
	registerUpgradeBackfillCleanup(t, db, scopeID, repoUnrouted)
	registerUpgradeBackfillCleanup(t, db, scopeID, repoAmbiguous)
	registerUpgradeBackfillCleanup(t, db, scopeID, repoNoData)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed exec %q: %v", q, err)
		}
	}

	now := time.Now().UTC()
	exec(`INSERT INTO ingestion_scopes
	  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	   observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3, '{}'::jsonb)`,
		scopeID, now, generationID)
	exec(`INSERT INTO scope_generations
	  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
	  VALUES ($1,$2,'manual',$3,$3,'active',$3)`, generationID, scopeID, now)

	// Positive case: PostsController.index has an exact matching route.
	seedRouteLivenessController(t, ctx, exec, repoRouted, "PostsController", "index")
	seedRouteLivenessRailsFile(t, ctx, exec, scopeID, generationID, repoRouted, []string{"PostsController.index"}, false)

	// Negative case: OrdersController.orphan has NO route, and the repo's
	// route surface is exact-only (no unmodeled routes anywhere).
	seedRouteLivenessController(t, ctx, exec, repoUnrouted, "OrdersController", "orphan")
	seedRouteLivenessRailsFile(t, ctx, exec, scopeID, generationID, repoUnrouted, []string{"OrdersController.index"}, false)

	// Ambiguous case: WidgetsController.orphan has no matching route_entries
	// handler, but the repo also registers a resources/resource macro
	// (has_unmodeled_routes=true) -- must stay confirmed.
	seedRouteLivenessController(t, ctx, exec, repoAmbiguous, "WidgetsController", "orphan")
	seedRouteLivenessRailsFile(t, ctx, exec, scopeID, generationID, repoAmbiguous, nil, true)

	// No-data case: no routes.rb fact at all for this repo.
	seedRouteLivenessController(t, ctx, exec, repoNoData, "GadgetsController", "orphan")

	for _, repoID := range []string{repoRouted, repoUnrouted, repoAmbiguous, repoNoData} {
		classes, err := store.loadCodeReachabilityRubyClasses(ctx, repoID)
		if err != nil {
			t.Fatalf("loadCodeReachabilityRubyClasses(%s): %v", repoID, err)
		}
		roots, err := store.loadCodeReachabilityRoots(ctx, repoID)
		if err != nil {
			t.Fatalf("loadCodeReachabilityRoots(%s): %v", repoID, err)
		}
		routes, err := store.loadCodeReachabilityRailsRouteFacts(ctx, repoID)
		if err != nil {
			t.Fatalf("loadCodeReachabilityRailsRouteFacts(%s): %v", repoID, err)
		}
		rows, _, _ := reducer.BuildCodeRootVerdicts(reducer.CodeReachabilityProjectionInput{
			ScopeID:      scopeID,
			GenerationID: generationID,
			RepositoryID: repoID,
			Roots:        roots,
			RubyClasses:  classes,
			RubyRoutes:   routes,
		})
		if len(rows) != 1 {
			t.Fatalf("repo %s: expected exactly one verdict row, got %+v", repoID, rows)
		}
		row := rows[0]
		switch repoID {
		case repoRouted:
			if row.Verdict != reducer.CodeRootVerdictConfirmed || row.Basis.RouteEvidence != reducer.RouteEvidenceRouted {
				t.Fatalf("routed repo: got verdict=%s route_evidence=%s, want confirmed/routed (basis=%+v)", row.Verdict, row.Basis.RouteEvidence, row.Basis)
			}
		case repoUnrouted:
			if row.Verdict != reducer.CodeRootVerdictDowngraded || row.Basis.Reason != reducer.ReasonRouteUnreachable {
				t.Fatalf("unrouted repo: got verdict=%s reason=%s, want downgraded/route_unreachable (basis=%+v)", row.Verdict, row.Basis.Reason, row.Basis)
			}
		case repoAmbiguous:
			if row.Verdict != reducer.CodeRootVerdictConfirmed || row.Basis.RouteEvidence != reducer.RouteEvidenceAmbiguous {
				t.Fatalf("ambiguous repo: got verdict=%s route_evidence=%s, want confirmed/unmodeled_routes_present (basis=%+v)", row.Verdict, row.Basis.RouteEvidence, row.Basis)
			}
		case repoNoData:
			if row.Verdict != reducer.CodeRootVerdictConfirmed || row.Basis.RouteEvidence != reducer.RouteEvidenceNoData {
				t.Fatalf("no-data repo: got verdict=%s route_evidence=%s, want confirmed/no_route_data (basis=%+v)", row.Verdict, row.Basis.RouteEvidence, row.Basis)
			}
		}
	}
}
