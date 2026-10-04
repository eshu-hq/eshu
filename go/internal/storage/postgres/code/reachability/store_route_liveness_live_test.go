// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/codeintel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
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
// codeintel.BuildCodeRootVerdicts, not a hand-built fixture.
func TestCodeReachabilityRailsRouteFactsLoaderRoundTrip(t *testing.T) {
	ctx, db := openRouteLivenessLiveDB(t)
	store := reachabilitystore.NewCodeReachabilityStore(postgres.SQLDB{DB: db})

	suffix := routeLivenessTestSuffix(t)
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	repoRouted := "repo-routed-" + suffix
	repoUnrouted := "repo-unrouted-" + suffix
	repoAmbiguous := "repo-ambiguous-" + suffix
	repoNoData := "repo-nodata-" + suffix

	registerRouteLivenessCleanup(t, db, scopeID, repoRouted)
	registerRouteLivenessCleanup(t, db, scopeID, repoUnrouted)
	registerRouteLivenessCleanup(t, db, scopeID, repoAmbiguous)
	registerRouteLivenessCleanup(t, db, scopeID, repoNoData)

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
		classes, err := reachabilitystore.LoadCodeReachabilityRubyClasses(store, ctx, repoID)
		if err != nil {
			t.Fatalf("loadCodeReachabilityRubyClasses(%s): %v", repoID, err)
		}
		roots, err := reachabilitystore.LoadCodeReachabilityRoots(store, ctx, repoID)
		if err != nil {
			t.Fatalf("loadCodeReachabilityRoots(%s): %v", repoID, err)
		}
		routes, err := reachabilitystore.LoadCodeReachabilityRailsRouteFacts(store, ctx, repoID)
		if err != nil {
			t.Fatalf("loadCodeReachabilityRailsRouteFacts(%s): %v", repoID, err)
		}
		rows, _, _ := codeintel.BuildCodeRootVerdicts(codeintel.CodeReachabilityProjectionInput{
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
			if row.Verdict != codeintel.CodeRootVerdictConfirmed || row.Basis.RouteEvidence != codeintel.RouteEvidenceRouted {
				t.Fatalf("routed repo: got verdict=%s route_evidence=%s, want confirmed/routed (basis=%+v)", row.Verdict, row.Basis.RouteEvidence, row.Basis)
			}
		case repoUnrouted:
			if row.Verdict != codeintel.CodeRootVerdictDowngraded || row.Basis.Reason != codeintel.ReasonRouteUnreachable {
				t.Fatalf("unrouted repo: got verdict=%s reason=%s, want downgraded/route_unreachable (basis=%+v)", row.Verdict, row.Basis.Reason, row.Basis)
			}
		case repoAmbiguous:
			if row.Verdict != codeintel.CodeRootVerdictConfirmed || row.Basis.RouteEvidence != codeintel.RouteEvidenceAmbiguous {
				t.Fatalf("ambiguous repo: got verdict=%s route_evidence=%s, want confirmed/unmodeled_routes_present (basis=%+v)", row.Verdict, row.Basis.RouteEvidence, row.Basis)
			}
		case repoNoData:
			if row.Verdict != codeintel.CodeRootVerdictConfirmed || row.Basis.RouteEvidence != codeintel.RouteEvidenceNoData {
				t.Fatalf("no-data repo: got verdict=%s route_evidence=%s, want confirmed/no_route_data (basis=%+v)", row.Verdict, row.Basis.RouteEvidence, row.Basis)
			}
		}
	}
}

// TestCodeReachabilityRailsRouteFactsLoaderKeepsRootOnlyRoutedController is the
// P0 fix regression (coordinator review of #5494, head 26ba26d2d): a
// controller routed ONLY via Rails' `root "welcome#index"` shorthand must stay
// CONFIRMED through the full real production path -- the loader reading the
// exact fact_records shape internal/parser/ruby/framework_routes.go now emits
// for a `root` route (empty route_entries, has_unmodeled_routes=true) feeding
// the real codeintel.BuildCodeRootVerdicts. Before the parser fix, `root` set
// neither an exact route_entries handler NOR has_unmodeled_routes, so this
// exact scenario would have silently downgraded WelcomeController#index to
// route_unreachable -- a live controller called dead.
func TestCodeReachabilityRailsRouteFactsLoaderKeepsRootOnlyRoutedController(t *testing.T) {
	ctx, db := openRouteLivenessLiveDB(t)
	store := reachabilitystore.NewCodeReachabilityStore(postgres.SQLDB{DB: db})

	suffix := routeLivenessTestSuffix(t)
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	repoID := "repo-root-routed-" + suffix

	registerRouteLivenessCleanup(t, db, scopeID, repoID)

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

	seedRouteLivenessController(t, ctx, exec, repoID, "WelcomeController", "index")
	// Empty route_entries (root produces no exact handler) + has_unmodeled_routes=true
	// -- exactly what framework_routes.go emits for `root "welcome#index"`.
	seedRouteLivenessRailsFile(t, ctx, exec, scopeID, generationID, repoID, nil, true)

	classes, err := reachabilitystore.LoadCodeReachabilityRubyClasses(store, ctx, repoID)
	if err != nil {
		t.Fatalf("loadCodeReachabilityRubyClasses: %v", err)
	}
	roots, err := reachabilitystore.LoadCodeReachabilityRoots(store, ctx, repoID)
	if err != nil {
		t.Fatalf("loadCodeReachabilityRoots: %v", err)
	}
	routes, err := reachabilitystore.LoadCodeReachabilityRailsRouteFacts(store, ctx, repoID)
	if err != nil {
		t.Fatalf("loadCodeReachabilityRailsRouteFacts: %v", err)
	}
	if !routes.HasUnmodeledRoutes {
		t.Fatalf("expected HasUnmodeledRoutes=true for the root-only-routed repo, got %+v", routes)
	}

	rows, downgraded, _ := codeintel.BuildCodeRootVerdicts(codeintel.CodeReachabilityProjectionInput{
		ScopeID:      scopeID,
		GenerationID: generationID,
		RepositoryID: repoID,
		Roots:        roots,
		RubyClasses:  classes,
		RubyRoutes:   routes,
	})
	if len(rows) != 1 {
		t.Fatalf("expected exactly one verdict row, got %+v", rows)
	}
	row := rows[0]
	if row.Verdict != codeintel.CodeRootVerdictConfirmed {
		t.Fatalf("root-only-routed WelcomeController#index: verdict = %s, want confirmed (basis=%+v)", row.Verdict, row.Basis)
	}
	if row.Basis.RouteEvidence != codeintel.RouteEvidenceAmbiguous {
		t.Fatalf("root-only-routed WelcomeController#index: route_evidence = %s, want %s", row.Basis.RouteEvidence, codeintel.RouteEvidenceAmbiguous)
	}
	if len(downgraded) != 0 {
		t.Fatalf("root-only-routed WelcomeController#index must not be downgraded, got %v", downgraded)
	}
}

// TestCodeReachabilityPendingInputsPlanAtEpochBump is the #7547 fixture-scale
// (800 repos, NOT QA-scale) EXPLAIN (ANALYZE, BUFFERS) of the pending-input
// loader with $2 = the bumped epoch, against a fixture of epoch-3 watermarks
// that all become stale at the bump. It asserts the plan keeps the node classes
// of the same query at the previous epoch (all watermarks current) and logs both
// plans and timings.
func TestCodeReachabilityPendingInputsPlanAtEpochBump(t *testing.T) {
	ctx, db := openRouteLivenessLiveDB(t)
	const prefix = "scope-explain7547-"
	exec := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"code_reachability_repository_watermarks", "shared_projection_intents", "shared_projection_acceptance", "fact_work_items", "scope_generations", "ingestion_scopes"} {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM "+table+" WHERE scope_id LIKE '"+prefix+"%'")
		}
	})
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	        observed_at, ingested_at, status, active_generation_id, payload)
	      SELECT '` + prefix + `' || g, 'repository', 'git', 'k' || g, 'git', 'k' || g, now(), now(), 'active', 'gen-explain7547-' || g, '{}'::jsonb
	      FROM generate_series(1, 800) g`)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
	      SELECT 'gen-explain7547-' || g, '` + prefix + `' || g, 'manual', now(), now(), 'active', now() FROM generate_series(1, 800) g`)
	exec(`INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
	      SELECT '` + prefix + `' || g, 'repo-explain7547-' || g, 'run-' || g, 'gen-explain7547-' || g, now() - interval '1 hour', now() - interval '1 hour'
	      FROM generate_series(1, 800) g`)
	exec(`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
	        source_run_id, generation_id, payload, created_at, completed_at)
	      SELECT 'intent-explain7547-' || g, 'code_calls', 'repo-explain7547-' || g, '` + prefix + `' || g, 'repo-explain7547-' || g, 'repo-explain7547-' || g,
	        'run-' || g, 'gen-explain7547-' || g, '{}'::jsonb, now() - interval '1 hour', now() - interval '1 hour'
	      FROM generate_series(1, 800) g`)
	// #7547 loader gate: each run carries both succeeded reducer
	// materialization work items, so all 800 runs stay candidates at the bump.
	exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, payload, created_at, updated_at)
	      SELECT 'wi-explain7547-' || g || '-' || d, '` + prefix + `' || g, 'gen-explain7547-' || g, 'reducer', d, 'succeeded', 1, '{}'::jsonb, now(), now()
	      FROM generate_series(1, 800) g, unnest(ARRAY['code_call_materialization', 'inheritance_materialization']) d`)
	exec(`INSERT INTO code_reachability_repository_watermarks (scope_id, generation_id, repository_id, truncated, updated_at, verdict_schema_epoch)
	      SELECT '` + prefix + `' || g, 'gen-explain7547-' || g, 'repo-explain7547-' || g, false, now() - interval '59 minutes', 3 FROM generate_series(1, 800) g`)
	exec(`ANALYZE ingestion_scopes; ANALYZE scope_generations; ANALYZE shared_projection_acceptance; ANALYZE shared_projection_intents; ANALYZE fact_work_items; ANALYZE code_reachability_repository_watermarks`)

	nodeClass := regexp.MustCompile(`(?m)(Nested Loop|Hash(?: Right| Left)? Join|Merge Join|Seq Scan|Index Only Scan|Index Scan|Bitmap Heap Scan|HashAggregate|GroupAggregate|Sort|Limit)`)
	explain := func(epoch int) (plan string, classes map[string]bool) {
		t.Helper()
		rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+reachabilitystore.ListPendingCodeReachabilityInputsSQL, 100, epoch)
		if err != nil {
			t.Fatalf("explain at epoch %d: %v", epoch, err)
		}
		defer func() { _ = rows.Close() }()
		var b strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			b.WriteString(line + "\n")
		}
		classes = map[string]bool{}
		for _, m := range nodeClass.FindAllString(b.String(), -1) {
			classes[m] = true
		}
		return b.String(), classes
	}
	prevPlan, prevClasses := explain(3)
	bumpPlan, bumpClasses := explain(reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
	t.Logf("fixture-scale (800 repos) plan at epoch 3 (all current):\n%s", prevPlan)
	t.Logf("fixture-scale (800 repos) plan at epoch %d (all stale, LIMIT 100):\n%s", reachabilitystore.CodeReachabilityVerdictSchemaEpoch, bumpPlan)
	if fmt.Sprint(prevClasses) != fmt.Sprint(bumpClasses) {
		t.Fatalf("plan node classes changed at the bump: epoch 3 %v vs epoch %d %v", prevClasses, reachabilitystore.CodeReachabilityVerdictSchemaEpoch, bumpClasses)
	}
}

// loaderGateBase sorts every #7547 fixture run ahead of real rows on a shared
// ESHU_POSTGRES_DSN (completed_at ASC), so LIMIT assertions stay exact.
var loaderGateBase = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// loaderGateIncompleteRuns are the runs the #7547 completeness gate must hold
// back: each has a completed intent and no watermark, so the pre-#7547
// statement schedules every one of them.
func loaderGateIncompleteRuns() []loaderGateRun {
	at := func(m int) time.Time { return loaderGateBase.Add(time.Duration(m) * time.Minute) }
	return []loaderGateRun{
		{name: "delta", isDelta: true, completedAt: at(1)},
		{name: "calls-failed", completedAt: at(2), workItems: map[string]string{
			"code_call_materialization": "failed", "inheritance_materialization": "succeeded",
		}},
		{name: "calls-retrying", completedAt: at(3), extraItem: [2]string{"code_call_materialization", "pending"}},
		{name: "calls-missing", completedAt: at(4), workItems: map[string]string{
			"inheritance_materialization": "succeeded",
		}},
		{name: "inheritance-missing", completedAt: at(5), workItems: map[string]string{
			"code_call_materialization": "succeeded",
		}},
		{name: "calls-pending", completedAt: at(6), pending: "code_calls"},
		{name: "inheritance-pending", completedAt: at(7), pending: "inheritance_edges"},
	}
}

// TestCodeReachabilityLoaderGateSkipsIncompleteRuns is the #7547 live proof
// that the loader schedules only runs whose edge set is provably complete
// (the dead-code run_gate): a delta generation, a materialization work item
// that is missing or not (only) succeeded, or a pending code_calls or
// inheritance_edges intent each keep the run out; a complete run stays in.
func TestCodeReachabilityLoaderGateSkipsIncompleteRuns(t *testing.T) {
	ctx, db := openRouteLivenessLiveDB(t)
	suffix := routeLivenessTestSuffix(t)
	epoch := reachabilitystore.CodeReachabilityVerdictSchemaEpoch

	incomplete := map[string]bool{}
	all := map[string]bool{}
	for _, run := range loaderGateIncompleteRuns() {
		scope := seedLoaderGateRun(t, ctx, db, suffix, run)
		incomplete[scope], all[scope] = true, true
	}
	complete := seedLoaderGateRun(t, ctx, db, suffix, loaderGateRun{name: "complete", completedAt: loaderGateBase})
	all[complete] = true

	// Non-vacuous: the pre-#7547 statement admits every incomplete run.
	legacy := queryLoaderGateRows(t, ctx, db, legacyListPendingCodeReachabilityInputsSQL, 100000, epoch, all)
	if len(legacy) != len(all) {
		t.Fatalf("legacy statement returned %d of %d seeded runs; fixtures are not admitted: %+v", len(legacy), len(all), legacy)
	}
	got := queryLoaderGateRows(t, ctx, db, reachabilitystore.ListPendingCodeReachabilityInputsSQL, 100000, epoch, all)
	for _, row := range got {
		if incomplete[row.ScopeID] {
			t.Errorf("incomplete run scheduled: %+v", row)
		}
	}
	if len(got) != 1 || got[0].ScopeID != complete {
		t.Fatalf("want only the complete run %q, got %+v", complete, got)
	}
}

// TestCodeReachabilityLoaderGateMatchesLegacyOnCompleteRuns is the #7547
// row-set differential: on complete runs the restructured statement returns
// exactly the pre-#7547 rows, order and LIMIT included, for epochs below, at,
// and above the stored watermark epochs. Fixtures cover a missing watermark,
// an older watermark, a newer watermark at a stale and at the current epoch,
// a superseded generation, an intent of another run, and a completed_at tie.
func TestCodeReachabilityLoaderGateMatchesLegacyOnCompleteRuns(t *testing.T) {
	ctx, db := openRouteLivenessLiveDB(t)
	suffix := routeLivenessTestSuffix(t)
	epoch := reachabilitystore.CodeReachabilityVerdictSchemaEpoch
	at := func(m int) time.Time { return loaderGateBase.Add(time.Duration(m) * time.Minute) }
	runs := []loaderGateRun{
		{name: "no-watermark", completedAt: at(1)},
		{name: "older-watermark", completedAt: at(2), watermark: &loaderGateWatermark{at(1), epoch}},
		{name: "stale-epoch", completedAt: at(3), watermark: &loaderGateWatermark{at(4), epoch - 1}},
		{name: "current", completedAt: at(4), watermark: &loaderGateWatermark{at(5), epoch}},
		{name: "old-generation", completedAt: at(5), oldGeneration: true},
		{name: "stray-run", completedAt: at(6), strayRunIntent: true},
		{name: "tie-b", completedAt: at(7)},
		{name: "tie-a", completedAt: at(7)},
	}
	scopes := map[string]bool{}
	for _, run := range runs {
		scopes[seedLoaderGateRun(t, ctx, db, suffix, run)] = true
	}
	for _, e := range []int{0, epoch, epoch + 1} {
		for _, limit := range []int{100000, 3} {
			want := queryLoaderGateRows(t, ctx, db, legacyListPendingCodeReachabilityInputsSQL, limit, e, scopes)
			got := queryLoaderGateRows(t, ctx, db, reachabilitystore.ListPendingCodeReachabilityInputsSQL, limit, e, scopes)
			if len(want) == 0 {
				t.Fatalf("epoch %d limit %d: legacy oracle returned no rows", e, limit)
			}
			if len(got) != len(want) {
				t.Fatalf("epoch %d limit %d: got %d rows, want %d\ngot  %+v\nwant %+v", e, limit, len(got), len(want), got, want)
			}
			for i := range want {
				if got[i].ScopeID != want[i].ScopeID || got[i].SourceRunID != want[i].SourceRunID ||
					got[i].GenerationID != want[i].GenerationID || got[i].RepositoryID != want[i].RepositoryID ||
					!got[i].CompletedAt.Equal(want[i].CompletedAt) {
					t.Fatalf("epoch %d limit %d row %d: got %+v want %+v", e, limit, i, got[i], want[i])
				}
			}
		}
	}
}

// TestCodeReachabilityLoaderGateEachPredicateIsLoadBearing seeds a mutation
// per gate predicate: the production statement with exactly that predicate
// removed must schedule exactly the incomplete runs only it holds back,
// so no predicate of CompleteRunGateSQL is dead weight in the fixtures above.
func TestCodeReachabilityLoaderGateEachPredicateIsLoadBearing(t *testing.T) {
	ctx, db := openRouteLivenessLiveDB(t)
	suffix := routeLivenessTestSuffix(t)
	epoch := reachabilitystore.CodeReachabilityVerdictSchemaEpoch

	scopeByName := map[string]string{}
	all := map[string]bool{}
	for _, run := range loaderGateIncompleteRuns() {
		scope := seedLoaderGateRun(t, ctx, db, suffix, run)
		scopeByName[run.name], all[scope] = scope, true
	}
	predicates := strings.Split(reachabilitystore.CompleteRunGateSQL, "\n      AND ")
	// Each predicate, in CompleteRunGateSQL order, and the runs only it rejects.
	onlyRejects := [][]string{
		{"delta"},
		{"calls-missing"},
		{"inheritance-missing"},
		{"calls-retrying"},
		{"calls-pending", "inheritance-pending"},
	}
	if len(predicates) != len(onlyRejects) {
		t.Fatalf("CompleteRunGateSQL has %d predicates, mutation table covers %d", len(predicates), len(onlyRejects))
	}
	for i, names := range onlyRejects {
		kept := make([]string, 0, len(predicates)-1)
		kept = append(kept, predicates[:i]...)
		kept = append(kept, predicates[i+1:]...)
		mutated := strings.Replace(reachabilitystore.ListPendingCodeReachabilityInputsSQL,
			reachabilitystore.CompleteRunGateSQL, strings.Join(kept, "\n      AND "), 1)
		if mutated == reachabilitystore.ListPendingCodeReachabilityInputsSQL {
			t.Fatalf("mutation %d did not change the statement", i)
		}
		got := queryLoaderGateRows(t, ctx, db, mutated, 100000, epoch, all)
		gotNames := map[string]bool{}
		for _, row := range got {
			gotNames[row.ScopeID] = true
		}
		ok := len(got) == len(names)
		for _, name := range names {
			ok = ok && gotNames[scopeByName[name]]
		}
		if !ok {
			t.Errorf("without predicate %d (%.40q...) want only runs %v scheduled, got %+v", i, predicates[i], names, got)
		}
	}
}
