// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 (#7126 PR3b-2a) disposable PostgreSQL 18 proof of the start-up fence check CheckFence: the bootstrapped database has all five migration 186 triggers firing (Supported); DROP TRIGGER of each in turn names exactly that one absent and re-applying migration 186 restores it; DISABLE, ENABLE REPLICA give tgenabled D and R and count as missing while ENABLE ALWAYS and ENABLE count as present, for a content_files trigger and the constraint trigger; a same-named trigger on another table does not count; a database with no schema reports all five missing without a catalog error; enrolled in the live-postgres-readiness runner; 0.05-1.1 s per proof on a local PostgreSQL 18.6 on 2026-10-16; wall time is logged, not gated
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// installedFenceTriggers reads the fence triggers the real migrations created,
// from the catalog rather than from CheckFence's list: every non-internal
// trigger on content_files that runs a config_artifact function, and every
// non-internal trigger on config_artifact_fence_pending.
func installedFenceTriggers(ctx context.Context, t *testing.T, db *sql.DB) []artifacts.FenceTrigger {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT c.relname::text, tg.tgname::text
FROM pg_trigger tg
JOIN pg_class c ON c.oid = tg.tgrelid
JOIN pg_proc p ON p.oid = tg.tgfoid
WHERE NOT tg.tgisinternal
  AND ((tg.tgrelid = 'content_files'::regclass AND p.proname LIKE 'config_artifact%')
    OR tg.tgrelid = 'config_artifact_fence_pending'::regclass)
ORDER BY 1, 2`)
	if err != nil {
		t.Fatalf("read installed fence triggers: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []artifacts.FenceTrigger
	for rows.Next() {
		var trigger artifacts.FenceTrigger
		if err := rows.Scan(&trigger.Table, &trigger.Name); err != nil {
			t.Fatal(err)
		}
		out = append(out, trigger)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) != 5 {
		t.Fatalf("installed fence triggers = %v, want the five of migration 186", out)
	}
	return out
}

// fenceMigrationSQL returns the embedded migration 186.
func fenceMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == "content_files_version_fence" {
			return def.SQL
		}
	}
	t.Fatal("migration content_files_version_fence not found")
	return ""
}

// wantMissing asserts CheckFence refuses with exactly want missing.
func wantMissing(ctx context.Context, t *testing.T, db *sql.DB, label string, want []artifacts.MissingFenceTrigger) {
	t.Helper()
	got := artifacts.CheckFence(ctx, postgres.SQLDB{DB: db}, nil)
	if got.Supported || got.Reason != artifacts.ReasonVersionFenceMissing || !reflect.DeepEqual(got.Missing, want) {
		t.Fatalf("%s: CheckFence = %+v, want unsupported %q missing %v",
			label, got, artifacts.ReasonVersionFenceMissing, want)
	}
}

// wantSupported asserts CheckFence finds all five triggers.
func wantSupported(ctx context.Context, t *testing.T, db *sql.DB, label string) {
	t.Helper()
	if got := artifacts.CheckFence(ctx, postgres.SQLDB{DB: db}, nil); !got.Supported || got.Reason != "" || len(got.Missing) != 0 {
		t.Fatalf("%s: CheckFence = %+v, want supported", label, got)
	}
}

// TestCheckFenceFindsTheMigratedTriggersLive runs the start-up check on a
// database the real bootstrap migrated: all five fence triggers are present
// and enabled.
func TestCheckFenceFindsTheMigratedTriggersLive(t *testing.T) {
	ctx, db := openDatabase(t)
	installedFenceTriggers(ctx, t, db)
	wantSupported(ctx, t, db, "bootstrapped database")
}

// TestCheckFenceNamesEachDroppedTriggerLive drops each installed fence
// trigger in turn: the check names exactly that one, and re-applying
// migration 186 restores it.
func TestCheckFenceNamesEachDroppedTriggerLive(t *testing.T) {
	ctx, db := openDatabase(t)
	migration := fenceMigrationSQL(t)
	for _, trigger := range installedFenceTriggers(ctx, t, db) {
		mustExec(ctx, t, db, "DROP TRIGGER "+trigger.Name+" ON "+trigger.Table)
		wantMissing(ctx, t, db, "drop "+trigger.Name, []artifacts.MissingFenceTrigger{{FenceTrigger: trigger}})
		mustExec(ctx, t, db, migration)
		wantSupported(ctx, t, db, "re-applied migration 186 after dropping "+trigger.Name)
	}
}

// TestCheckFenceIgnoresASameNamedTriggerOnAnotherTableLive drops the real
// insert fence and creates a trigger of the same name on another table: that
// trigger does not fence content_files, so the check still names it absent.
func TestCheckFenceIgnoresASameNamedTriggerOnAnotherTableLive(t *testing.T) {
	ctx, db := openDatabase(t)
	insert := artifacts.FenceTrigger{Table: "content_files", Name: "content_files_version_fence_insert"}
	mustExec(ctx, t, db,
		"DROP TRIGGER "+insert.Name+" ON content_files",
		"CREATE TABLE fence_decoy (id int)",
		"CREATE FUNCTION fence_decoy_noop() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NULL; END$$",
		"CREATE TRIGGER "+insert.Name+" AFTER INSERT ON fence_decoy FOR EACH STATEMENT EXECUTE FUNCTION fence_decoy_noop()")
	wantMissing(ctx, t, db, "same-named trigger on fence_decoy", []artifacts.MissingFenceTrigger{{FenceTrigger: insert}})
}

// TestCheckFenceTracksTriggerEnablementLive walks one statement trigger and
// the constraint trigger through ALTER TABLE ... DISABLE, ENABLE ALWAYS,
// ENABLE REPLICA and ENABLE: only origin ('O') and always ('A') count.
func TestCheckFenceTracksTriggerEnablementLive(t *testing.T) {
	ctx, db := openDatabase(t)
	for _, trigger := range []artifacts.FenceTrigger{
		{Table: "content_files", Name: "content_files_version_fence_insert"},
		{Table: "config_artifact_fence_pending", Name: "config_artifact_fence_pending_commit"},
	} {
		alter := "ALTER TABLE " + trigger.Table + " "
		mustExec(ctx, t, db, alter+"DISABLE TRIGGER "+trigger.Name)
		wantMissing(ctx, t, db, "disable "+trigger.Name,
			[]artifacts.MissingFenceTrigger{{FenceTrigger: trigger, Enabled: "D"}})
		mustExec(ctx, t, db, alter+"ENABLE ALWAYS TRIGGER "+trigger.Name)
		wantSupported(ctx, t, db, "enable always "+trigger.Name)
		mustExec(ctx, t, db, alter+"ENABLE REPLICA TRIGGER "+trigger.Name)
		wantMissing(ctx, t, db, "enable replica "+trigger.Name,
			[]artifacts.MissingFenceTrigger{{FenceTrigger: trigger, Enabled: "R"}})
		mustExec(ctx, t, db, alter+"ENABLE TRIGGER "+trigger.Name)
		wantSupported(ctx, t, db, "enable "+trigger.Name)
	}
}

// TestCheckFenceReportsAnUnmigratedDatabaseAsMissingLive runs the check on a
// database with no Eshu schema: the tables do not exist, so all five are
// missing and the catalog read itself does not fail.
func TestCheckFenceReportsAnUnmigratedDatabaseAsMissingLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"), 5*time.Minute)
	got := artifacts.CheckFence(ctx, postgres.SQLDB{DB: db}, nil)
	if got.Supported || got.Reason != artifacts.ReasonVersionFenceMissing || len(got.Missing) != 5 {
		t.Fatalf("CheckFence(unmigrated database) = %+v, want all five missing", got)
	}
	for _, missing := range got.Missing {
		if missing.Enabled != "" {
			t.Fatalf("CheckFence(unmigrated database) missing %+v, want absent", missing)
		}
	}
}
