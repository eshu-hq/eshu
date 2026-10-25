// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8961 disposable PostgreSQL 18 proof that the sbt key repair resumes at its cursor after a failed page, returns a lock timeout without advancing the cursor or inserting anything while an ingester holds the scope row, and never inserts a key from a payload an ingester is rewriting under that row; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/consumption"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func sbtRepairProgress(t *testing.T, ctx context.Context, database *sql.DB, name string) string {
	t.Helper()
	values := sbtRepairStrings(t, ctx, database,
		`SELECT cursor_scope_id FROM package_manifest_consumption_key_backfill_progress WHERE marker_name = $1`, name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func newSBTRepairForTest(database *sql.DB) consumption.SBTRepair {
	return (PackageManifestConsumptionKeyBackfill{}).sbtRepair(SQLDB{DB: database})
}

// TestPackageManifestSBTRepairResumesAtItsCursorAfterAFailedPage fails the
// repair on the first page of the second scope. The first scope's keys and the
// cursor stay committed; the next pass starts after the cursor, not at the top.
func TestPackageManifestSBTRepairResumesAtItsCursorAfterAFailedPage(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	markSBTRepairV2Ready(t, ctx, database)
	for _, scope := range []string{"scala-a", "scala-b"} {
		seedSBTRepairScope(t, ctx, database, scope)
		for _, fact := range []string{"1", "2"} {
			seedSBTRepairFact(t, ctx, database, scope, scope+"-fact-"+fact, "o:n", map[string]any{
				"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "o:n_3",
			}, false)
		}
	}

	repair := newSBTRepairForTest(database)
	repair.PageSize = 1 // two facts per scope: a full page, then a page that ends the scope
	load := repair.LoadPage
	repair.LoadPage = func(ctx context.Context, queryer db.Queryer, scopeID, after string) ([]facts.Envelope, error) {
		envelopes, err := load(ctx, queryer, scopeID, after)
		if len(envelopes) > repair.PageSize {
			envelopes = envelopes[:repair.PageSize]
		}
		return envelopes, err
	}
	insertCalls := 0
	insert := repair.Insert
	repair.Insert = func(ctx context.Context, tx db.ExecQueryer, envelopes []facts.Envelope) (int64, error) {
		insertCalls++
		// Each scope takes three page transactions here: scala-a is calls 1 to 3,
		// scala-b's first page is call 4 and its second is call 5.
		if insertCalls == 5 {
			return 0, errors.New("injected failure on scala-b's second page")
		}
		return insert(ctx, tx, envelopes)
	}

	_, err := repair.Pass(ctx, SQLDB{DB: database})
	if err == nil {
		t.Fatal("Pass() error = nil, want the injected failure")
	}
	if cursor := sbtRepairProgress(t, ctx, database, consumption.SBTRepairMarker); cursor != "scala-a" {
		t.Fatalf("cursor after the failure = %q, want scala-a (the last finished scope)", cursor)
	}
	if keys := sbtRepairKeys(t, ctx, database, "scala-a"); len(keys) != 4 {
		t.Fatalf("scala-a keys = %v, want 4 (2 facts x 2 name forms)", keys)
	}

	repair.Insert = insert
	var listedAfter []string
	list := repair.ListScopes
	repair.ListScopes = func(ctx context.Context, queryer db.Queryer, after string, limit int) ([]string, error) {
		listedAfter = append(listedAfter, after)
		return list(ctx, queryer, after, limit)
	}
	report, err := repair.Pass(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("resumed Pass(): %v", err)
	}
	if !reflect.DeepEqual(listedAfter, []string{"scala-a"}) {
		t.Fatalf("resumed listing started after %v, want [scala-a]", listedAfter)
	}
	if got := sbtRepairKeys(t, ctx, database, "scala-b"); len(got) != 4 {
		t.Fatalf("scala-b keys = %v, want 4 after the resume", got)
	}
	// Walk one inserted rows (scala-a before the crash, scala-b after), so the
	// walk restarts instead of writing the marker.
	if !report.Pending || !report.Restarted || sbtRepairMarkerPresent(t, ctx, database) {
		t.Fatalf("report = %+v, want a restarted walk and no marker yet", report)
	}
	if total := sbtRepairProgress(t, ctx, database, consumption.SBTRepairMarker+":inserted"); total != "0" {
		t.Fatalf("insert total after the restart = %q, want 0", total)
	}
}

// TestPackageManifestSBTRepairReturnsLockTimeoutWithoutAdvancing holds the
// scope row the way an ingester does. The repair fails with SQLSTATE 55P03,
// inserts nothing, and leaves the cursor; once the row is released a rerun
// succeeds.
func TestPackageManifestSBTRepairReturnsLockTimeoutWithoutAdvancing(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	markSBTRepairV2Ready(t, ctx, database)
	seedSBTRepairScope(t, ctx, database, "scala-repo")
	seedSBTRepairFact(t, ctx, database, "scala-repo", "sbt-binary", "org.typelevel:cats-core", map[string]any{
		"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "org.typelevel:cats-core_2.13",
	}, false)

	holder, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	held := true
	defer func() {
		if held {
			_ = holder.Rollback()
		}
	}()
	if _, err := holder.ExecContext(ctx, `SELECT scope_id FROM ingestion_scopes WHERE scope_id = 'scala-repo' FOR NO KEY UPDATE`); err != nil {
		t.Fatalf("hold scope row: %v", err)
	}

	repair := newSBTRepairForTest(database)
	repair.LockTimeout = 300 * time.Millisecond
	report, err := repair.Pass(ctx, SQLDB{DB: database})
	if err == nil || !coordination.IsLockNotAvailable(err) {
		t.Fatalf("Pass() error = %v, want SQLSTATE 55P03", err)
	}
	if report.MaxLockWait < 250*time.Millisecond {
		t.Fatalf("MaxLockWait = %v, want about the 300ms lock timeout", report.MaxLockWait)
	}
	if cursor := sbtRepairProgress(t, ctx, database, consumption.SBTRepairMarker); cursor != "" {
		t.Fatalf("cursor after the timeout = %q, want unchanged", cursor)
	}
	if keys := sbtRepairKeys(t, ctx, database, "scala-repo"); len(keys) != 0 {
		t.Fatalf("keys after the timeout = %v, want none", keys)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatalf("release scope row: %v", err)
	}
	held = false
	if _, err := repair.Pass(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("rerun Pass(): %v", err)
	}
	if got := sbtRepairKeys(t, ctx, database, "scala-repo"); !reflect.DeepEqual(got, sbtRepairWantKeys) {
		t.Fatalf("keys after the rerun = %v, want %v", got, sbtRepairWantKeys)
	}
}

// TestPackageManifestSBTRepairNeverInsertsAStaleKeyBehindARewrite has an
// ingester-style writer hold the scope row, rewrite the fact's payload to a
// Scala 3 artifact, and refresh its keys, while the repair is blocked on the
// same row. The repair must read the rewritten payload after the writer
// commits: no key for the old artifact may exist at the end.
func TestPackageManifestSBTRepairNeverInsertsAStaleKeyBehindARewrite(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	markSBTRepairV2Ready(t, ctx, database)
	seedSBTRepairScope(t, ctx, database, "scala-repo")
	seedSBTRepairFact(t, ctx, database, "scala-repo", "sbt-binary", "org.typelevel:cats-core", map[string]any{
		"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "org.typelevel:cats-core_2.13",
	}, true)
	if _, err := database.ExecContext(ctx, `DELETE FROM package_manifest_consumption_keys WHERE scope_id = 'scala-repo'`); err != nil {
		t.Fatalf("clear keys to simulate a stored keyless row: %v", err)
	}

	writer, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin writer: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = writer.Rollback()
		}
	}()
	if _, err := writer.ExecContext(ctx, setPackageManifestConsumptionKeysWriterSQL); err != nil {
		t.Fatalf("set writer: %v", err)
	}
	if _, err := writer.ExecContext(ctx, `SELECT scope_id FROM ingestion_scopes WHERE scope_id = 'scala-repo' FOR NO KEY UPDATE`); err != nil {
		t.Fatalf("writer holds scope row: %v", err)
	}
	rewritten := sbtRepairPayload("scala-repo", "org.typelevel:cats-core", map[string]any{
		"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "org.typelevel:cats-core_3",
	})
	if _, err := writer.ExecContext(ctx, `UPDATE fact_records SET payload = $2::jsonb WHERE fact_id = $1`,
		"sbt-binary", mustJSON(t, rewritten)); err != nil {
		t.Fatalf("rewrite payload: %v", err)
	}
	if err := refreshPackageManifestConsumptionKeys(ctx, writer, []facts.Envelope{{
		FactID: "sbt-binary", ScopeID: "scala-repo", GenerationID: "scala-repo-gen", FactKind: "content_entity",
		Payload: rewritten, SourceRef: facts.Ref{SourceSystem: "git"},
	}}); err != nil {
		t.Fatalf("writer refresh keys: %v", err)
	}

	passDone := make(chan error, 1)
	go func() {
		_, err := newSBTRepairForTest(database).Pass(ctx, SQLDB{DB: database})
		passDone <- err
	}()
	// Wait until the repair is queued on the scope row, then let the writer commit.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		waiting := sbtRepairStrings(t, ctx, database,
			`SELECT 'w' FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%FOR NO KEY UPDATE%' AND pid <> pg_backend_pid()`)
		if len(waiting) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("commit writer: %v", err)
	}
	committed = true
	if err := <-passDone; err != nil {
		t.Fatalf("Pass(): %v", err)
	}

	want := []string{
		"sbt-binary|maven|org.typelevel/cats-core_3",
		"sbt-binary|maven|org.typelevel:cats-core_3",
	}
	if got := sbtRepairKeys(t, ctx, database, "scala-repo"); !reflect.DeepEqual(got, want) {
		t.Fatalf("keys = %v, want only the rewritten artifact's %v", got, want)
	}
}
