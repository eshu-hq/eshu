// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fenceRow is one pg_trigger row the fake catalog returns: the table and
// trigger name the query asked for and that trigger's tgenabled.
type fenceRow struct {
	table, name, enabled string
}

// fenceQueryer answers the fence catalog query with fixed rows or an error,
// and records the query and its arguments.
type fenceQueryer struct {
	rows    []fenceRow
	err     error
	rowsErr error
	query   string
	args    []any
	calls   int
}

func (q *fenceQueryer) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q.calls++
	q.query = query
	q.args = args
	if q.err != nil {
		return nil, q.err
	}
	return &fenceRows{rows: q.rows, err: q.rowsErr}, nil
}

// fenceRows is a three-column db.Rows over fixed fence rows.
type fenceRows struct {
	rows []fenceRow
	next int
	err  error
}

func (r *fenceRows) Next() bool { r.next++; return r.next <= len(r.rows) }
func (r *fenceRows) Scan(dest ...any) error {
	row := r.rows[r.next-1]
	*dest[0].(*string) = row.table
	*dest[1].(*string) = row.name
	*dest[2].(*string) = row.enabled
	return nil
}
func (r *fenceRows) Err() error   { return r.err }
func (r *fenceRows) Close() error { return nil }

// allFenceRows returns one row per required trigger with tgenabled 'O', the
// catalog of a database that applied migration 186.
func allFenceRows() []fenceRow {
	out := make([]fenceRow, 0, len(fenceTriggers))
	for _, trigger := range fenceTriggers {
		out = append(out, fenceRow{trigger.Table, trigger.Name, "O"})
	}
	return out
}

// withEnabled returns allFenceRows with the tgenabled of name replaced.
func withEnabled(name, enabled string) []fenceRow {
	rows := allFenceRows()
	for i := range rows {
		if rows[i].name == name {
			rows[i].enabled = enabled
		}
	}
	return rows
}

// without returns allFenceRows with the row of name removed.
func without(name string) []fenceRow {
	var rows []fenceRow
	for _, row := range allFenceRows() {
		if row.name != name {
			rows = append(rows, row)
		}
	}
	return rows
}

func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestFenceTriggersListsTheFiveMigration186Triggers(t *testing.T) {
	t.Parallel()

	want := []FenceTrigger{
		{"content_files", "content_files_version_fence_insert"},
		{"content_files", "content_files_version_fence_update"},
		{"content_files", "content_files_version_fence_delete"},
		{"content_files", "content_files_version_fence_truncate"},
		{"config_artifact_fence_pending", "config_artifact_fence_pending_commit"},
	}
	if !reflect.DeepEqual(fenceTriggers, want) {
		t.Fatalf("fenceTriggers = %v, want %v", fenceTriggers, want)
	}
}

func TestCheckFenceAcceptsAllFiveEnabledTriggers(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	queryer := &fenceQueryer{rows: allFenceRows()}
	got := CheckFence(context.Background(), queryer, jsonLogger(&buf))

	if !got.Supported || got.Reason != "" || len(got.Missing) != 0 {
		t.Fatalf("CheckFence(all five) = %+v, want supported with no reason and nothing missing", got)
	}
	if queryer.calls != 1 || queryer.query != fenceTriggersSQL {
		t.Fatalf("calls = %d query = %q, want one round trip of fenceTriggersSQL", queryer.calls, queryer.query)
	}
	var tables, names []string
	for _, trigger := range fenceTriggers {
		tables = append(tables, trigger.Table)
		names = append(names, trigger.Name)
	}
	if !reflect.DeepEqual(queryer.args, []any{tables, names}) {
		t.Fatalf("args = %v, want the table and trigger name arrays of fenceTriggers", queryer.args)
	}
	records := logRecords(t, &buf)
	if len(records) != 1 || records[0]["level"] != "INFO" ||
		records[0]["event_name"] != fenceEventName || records[0]["fence_triggers"] != float64(len(fenceTriggers)) {
		t.Fatalf("log records = %v, want one INFO %s with fence_triggers=5", records, fenceEventName)
	}
}

// TestCheckFenceNamesEachMissingTrigger drops each trigger in turn: the check
// must refuse and name exactly that trigger as absent.
func TestCheckFenceNamesEachMissingTrigger(t *testing.T) {
	t.Parallel()

	for _, trigger := range fenceTriggers {
		t.Run(trigger.Name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			got := CheckFence(context.Background(), &fenceQueryer{rows: without(trigger.Name)}, jsonLogger(&buf))
			want := []MissingFenceTrigger{{FenceTrigger: trigger}}
			if got.Supported || got.Reason != ReasonVersionFenceMissing || !reflect.DeepEqual(got.Missing, want) {
				t.Fatalf("CheckFence(without %s) = %+v, want unsupported %q missing %v",
					trigger.Name, got, ReasonVersionFenceMissing, want)
			}
			assertMissingLog(t, &buf, []string{trigger.Table + "." + trigger.Name + "=absent"})
		})
	}
}

// TestCheckFenceCountsADisabledOrReplicaTriggerAsMissing: a trigger that
// exists but does not fire for an ordinary session ('D' disabled, 'R'
// replica-only) leaves writes unfenced, so it is missing; 'A' (always) fires
// and counts as present.
func TestCheckFenceCountsADisabledOrReplicaTriggerAsMissing(t *testing.T) {
	t.Parallel()

	insert := fenceTriggers[0]
	for _, enabled := range []string{"D", "R"} {
		var buf bytes.Buffer
		got := CheckFence(context.Background(), &fenceQueryer{rows: withEnabled(insert.Name, enabled)}, jsonLogger(&buf))
		want := []MissingFenceTrigger{{FenceTrigger: insert, Enabled: enabled}}
		if got.Supported || got.Reason != ReasonVersionFenceMissing || !reflect.DeepEqual(got.Missing, want) {
			t.Fatalf("CheckFence(%s tgenabled=%s) = %+v, want unsupported missing %v", insert.Name, enabled, got, want)
		}
		assertMissingLog(t, &buf, []string{insert.Table + "." + insert.Name + "=" + enabled})
	}

	got := CheckFence(context.Background(), &fenceQueryer{rows: withEnabled(insert.Name, "A")}, nil)
	if !got.Supported {
		t.Fatalf("CheckFence(%s tgenabled=A) = %+v, want supported", insert.Name, got)
	}
}

// TestCheckFenceReportsEveryMissingTriggerInListOrder: an empty catalog (the
// migration never ran) names all five, in fenceTriggers order.
func TestCheckFenceReportsEveryMissingTriggerInListOrder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	got := CheckFence(context.Background(), &fenceQueryer{}, jsonLogger(&buf))
	if got.Supported || got.Reason != ReasonVersionFenceMissing || len(got.Missing) != len(fenceTriggers) {
		t.Fatalf("CheckFence(empty catalog) = %+v, want all %d missing", got, len(fenceTriggers))
	}
	var want []string
	for i, trigger := range fenceTriggers {
		if got.Missing[i].FenceTrigger != trigger {
			t.Fatalf("Missing[%d] = %v, want %v", i, got.Missing[i], trigger)
		}
		want = append(want, trigger.Table+"."+trigger.Name+"=absent")
	}
	assertMissingLog(t, &buf, want)
}

// TestCheckFenceIgnoresATriggerOnTheWrongTable: a same-named trigger on
// another table does not fence content_files.
func TestCheckFenceIgnoresATriggerOnTheWrongTable(t *testing.T) {
	t.Parallel()

	rows := allFenceRows()
	rows[0].table = "content_entities"
	got := CheckFence(context.Background(), &fenceQueryer{rows: rows}, nil)
	if got.Supported || len(got.Missing) != 1 || got.Missing[0].FenceTrigger != fenceTriggers[0] {
		t.Fatalf("CheckFence(insert trigger on content_entities) = %+v, want %v missing", got, fenceTriggers[0])
	}
}

// TestCheckFenceFailsClosedOnAQueryError keeps the read model off when the
// catalog cannot be read: rows must not be trusted behind an unproven fence.
func TestCheckFenceFailsClosedOnAQueryError(t *testing.T) {
	t.Parallel()

	for name, queryer := range map[string]*fenceQueryer{
		"query": {err: errors.New("connection reset")},
		"rows":  {rows: allFenceRows(), rowsErr: errors.New("connection reset")},
	} {
		var buf bytes.Buffer
		got := CheckFence(context.Background(), queryer, jsonLogger(&buf))
		if got.Supported || got.Reason != ReasonFenceCheckFailed || len(got.Missing) != 0 {
			t.Fatalf("%s: CheckFence(error) = %+v, want unsupported with reason %q", name, got, ReasonFenceCheckFailed)
		}
		records := logRecords(t, &buf)
		if len(records) != 1 || records[0]["level"] != "ERROR" || records[0]["event_name"] != fenceEventName ||
			records[0]["reason"] != ReasonFenceCheckFailed ||
			records[0]["config_artifacts_status"] != StatusNotInstalled ||
			!strings.Contains(records[0]["error"].(string), "connection reset") {
			t.Fatalf("%s: log records = %v, want one ERROR naming the check failure and its error", name, records)
		}
	}
}

func TestCheckFenceToleratesANilLogger(t *testing.T) {
	t.Parallel()

	if got := CheckFence(context.Background(), &fenceQueryer{}, nil); got.Supported {
		t.Fatalf("CheckFence(empty catalog, nil logger) = %+v, want unsupported", got)
	}
	if got := CheckFence(context.Background(), &fenceQueryer{err: errors.New("down")}, nil); got.Supported {
		t.Fatalf("CheckFence(error, nil logger) = %+v, want unsupported", got)
	}
	if got := CheckFence(context.Background(), &fenceQueryer{rows: allFenceRows()}, nil); !got.Supported {
		t.Fatalf("CheckFence(all five, nil logger) = %+v, want supported", got)
	}
}

// assertMissingLog checks the one ERROR record of a missing-trigger result.
func assertMissingLog(t *testing.T, buf *bytes.Buffer, wantMissing []string) {
	t.Helper()
	records := logRecords(t, buf)
	if len(records) != 1 {
		t.Fatalf("log records = %v, want one", records)
	}
	record := records[0]
	var missing []string
	if raw, ok := record["missing_triggers"].([]any); ok {
		for _, value := range raw {
			missing = append(missing, value.(string))
		}
	}
	if record["level"] != "ERROR" || record["event_name"] != fenceEventName ||
		record["config_artifacts_status"] != StatusNotInstalled ||
		record["reason"] != ReasonVersionFenceMissing ||
		record["outcome"] != string(OutcomeFenceMissing) ||
		!reflect.DeepEqual(missing, wantMissing) {
		t.Fatalf("log record = %v, want ERROR %s not_installed %s missing_triggers=%v",
			record, fenceEventName, ReasonVersionFenceMissing, wantMissing)
	}
}
