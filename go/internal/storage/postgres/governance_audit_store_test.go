package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func TestBootstrapDefinitionsIncludeGovernanceAuditEvents(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	if len(defs) != 43 {
		t.Fatalf("BootstrapDefinitions() len = %d, want 43", len(defs))
	}
	var audit Definition
	for _, def := range defs {
		if def.Name == "governance_audit_events" {
			audit = def
			break
		}
	}
	if audit.Name == "" {
		t.Fatal("governance_audit_events definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS governance_audit_events",
		"event_id TEXT PRIMARY KEY",
		"correlation_id TEXT NULL",
		"governance_audit_events_query_idx",
		"governance_audit_events_correlation_idx",
		"governance_audit_events_reason_idx",
	} {
		if !strings.Contains(audit.SQL, want) {
			t.Fatalf("governance audit schema missing %q:\n%s", want, audit.SQL)
		}
	}
	for _, forbidden := range []string{"raw_body", "provider_response", "prompt_text", "credential_handle"} {
		if strings.Contains(audit.SQL, forbidden) {
			t.Fatalf("governance audit schema stores forbidden field %q:\n%s", forbidden, audit.SQL)
		}
	}
}

func TestGovernanceAuditStoreAppendNormalizesAndDeduplicatesRetry(t *testing.T) {
	t.Parallel()

	db := newGovernanceAuditMemoryDB()
	store := NewGovernanceAuditStore(db)
	event := governanceAuditStoreTestEvent()

	if err := store.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
		t.Fatalf("Append first event error = %v, want nil", err)
	}
	if err := store.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
		t.Fatalf("Append duplicate retry error = %v, want nil", err)
	}
	if got, want := len(db.rows), 1; got != want {
		t.Fatalf("stored rows = %d, want %d", got, want)
	}
	if got := db.lastRow().occurredAt.Location(); got != time.UTC {
		t.Fatalf("occurred_at location = %v, want UTC", got)
	}
	for _, exec := range db.execs {
		for _, arg := range exec.args {
			if value, ok := arg.(string); ok && strings.Contains(value, "unsafe-token") {
				t.Fatalf("Append leaked unsafe token in args: %q", value)
			}
		}
		if !strings.Contains(exec.query, "ON CONFLICT (event_id) DO NOTHING") {
			t.Fatalf("Append query is not idempotent:\n%s", exec.query)
		}
	}
}

func TestGovernanceAuditStoreAppendRejectsUnsafeEventWithoutEcho(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewGovernanceAuditStore(db)
	event := governanceAuditStoreTestEvent()
	event.ActorIDHash = "Bearer unsafe-token"

	err := store.Append(context.Background(), []governanceaudit.Event{event})
	if err == nil {
		t.Fatal("Append error = nil, want unsafe event rejection")
	}
	if strings.Contains(err.Error(), "unsafe-token") || strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("Append error exposed rejected value: %v", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("ExecContext calls = %d, want 0 for rejected event", got)
	}
}

func TestGovernanceAuditStoreListRequiresOperatorAuthorization(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewGovernanceAuditStore(db)

	_, err := store.List(context.Background(), GovernanceAuditQuery{
		EventType: governanceaudit.EventTypeReadAuthorization,
		Limit:     10,
	})
	if err == nil {
		t.Fatal("List error = nil, want unauthorized detailed query rejection")
	}
	if strings.Contains(err.Error(), "repository") || strings.Contains(err.Error(), "token") {
		t.Fatalf("List error exposed unsafe detail: %v", err)
	}
	if got := len(db.queries); got != 0 {
		t.Fatalf("QueryContext calls = %d, want 0 for unauthorized query", got)
	}
}

func TestGovernanceAuditStoreListAppliesBoundsAndOrdering(t *testing.T) {
	t.Parallel()

	now := governanceAuditStoreTestTime()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			string(governanceaudit.EventTypeReadAuthorization),
			string(governanceaudit.ActorClassScopedToken),
			sql.NullString{String: "sha256:aaaaaaaaaaaaaaaa", Valid: true},
			sql.NullString{},
			string(governanceaudit.ScopeClassRepository),
			sql.NullString{String: "sha256:bbbbbbbbbbbbbbbb", Valid: true},
			string(governanceaudit.DecisionDenied),
			"subject_scope_missing",
			sql.NullString{String: "corr:read-denied-1", Valid: true},
			sql.NullString{String: "sha256:cccccccccccccccc", Valid: true},
			now,
		}}}},
	}
	store := NewGovernanceAuditStore(db)

	events, err := store.List(context.Background(), GovernanceAuditQuery{
		OperatorAuthorized: true,
		EventType:          governanceaudit.EventTypeReadAuthorization,
		ActorClass:         governanceaudit.ActorClassScopedToken,
		ScopeClass:         governanceaudit.ScopeClassRepository,
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         "subject_scope_missing",
		CorrelationID:      "corr:read-denied-1",
		OccurredAfter:      now.Add(-time.Hour),
		OccurredBefore:     now.Add(time.Hour),
		Limit:              25,
	})
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("events len = %d, want %d", got, want)
	}
	if got, want := events[0].CorrelationID, "corr:read-denied-1"; got != want {
		t.Fatalf("CorrelationID = %q, want %q", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"event_type = $",
		"actor_class = $",
		"scope_class = $",
		"decision = $",
		"reason_code = $",
		"correlation_id = $",
		"occurred_at >= $",
		"occurred_at < $",
		"ORDER BY occurred_at ASC, event_id ASC",
		"LIMIT $",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("List query missing %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{"raw_body", "provider_response", "prompt_text", "credential_handle"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("List query exposes forbidden column %q:\n%s", forbidden, query)
		}
	}
}

func TestGovernanceAuditStoreDeleteExpiredUsesCutoff(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 3}}}
	store := NewGovernanceAuditStore(db)
	cutoff := governanceAuditStoreTestTime().Add(-24 * time.Hour)

	deleted, err := store.DeleteExpired(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("DeleteExpired error = %v, want nil", err)
	}
	if got, want := deleted, int64(3); got != want {
		t.Fatalf("deleted rows = %d, want %d", got, want)
	}
	query := db.execs[0].query
	if !strings.Contains(query, "DELETE FROM governance_audit_events") ||
		!strings.Contains(query, "occurred_at < $1") {
		t.Fatalf("DeleteExpired query missing retention cutoff:\n%s", query)
	}
	if got, want := db.execs[0].args[0].(time.Time), cutoff.UTC(); !got.Equal(want) {
		t.Fatalf("cutoff arg = %v, want %v", got, want)
	}
}

func TestGovernanceAuditStoreSummaryAggregatesWithoutBodies(t *testing.T) {
	t.Parallel()

	now := governanceAuditStoreTestTime()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{"total", "", int64(4), now},
			{"decision", string(governanceaudit.DecisionAllowed), int64(1), now},
			{"decision", string(governanceaudit.DecisionDenied), int64(2), now},
			{"decision", string(governanceaudit.DecisionUnavailable), int64(1), now},
			{"event_type", string(governanceaudit.EventTypeReadAuthorization), int64(2), now},
			{"actor_class", string(governanceaudit.ActorClassScopedToken), int64(2), now},
			{"scope_class", string(governanceaudit.ScopeClassRepository), int64(2), now},
			{"reason", "subject_scope_missing", int64(2), now},
		}}},
	}
	store := NewGovernanceAuditStore(db)

	summary, err := store.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary error = %v, want nil", err)
	}
	if got, want := summary.Total, 4; got != want {
		t.Fatalf("Total = %d, want %d", got, want)
	}
	if got, want := summary.Denied, 2; got != want {
		t.Fatalf("Denied = %d, want %d", got, want)
	}
	if got, want := summary.Unavailable, 1; got != want {
		t.Fatalf("Unavailable = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, forbidden := range []string{
		"actor_id_hash",
		"scope_id_hash",
		"service_principal_id",
		"policy_revision_hash",
		"correlation_id",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("Summary query exposes detailed column %q:\n%s", forbidden, query)
		}
	}
}

func governanceAuditStoreTestEvent() governanceaudit.Event {
	return governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         governanceaudit.ActorClassScopedToken,
		ActorIDHash:        "sha256:aaaaaaaaaaaaaaaa",
		ScopeClass:         governanceaudit.ScopeClassRepository,
		ScopeIDHash:        "sha256:bbbbbbbbbbbbbbbb",
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         "subject_scope_missing",
		CorrelationID:      "corr:read-denied-1",
		PolicyRevisionHash: "sha256:cccccccccccccccc",
		OccurredAt:         governanceAuditStoreTestTime(),
	}
}

func governanceAuditStoreTestTime() time.Time {
	return time.Date(2026, time.June, 9, 17, 0, 0, 0, time.UTC)
}

type governanceAuditMemoryRow struct {
	eventID            string
	eventType          string
	actorClass         string
	actorIDHash        string
	servicePrincipal   string
	scopeClass         string
	scopeIDHash        string
	decision           string
	reasonCode         string
	correlationID      string
	policyRevisionHash string
	occurredAt         time.Time
	persistedAt        time.Time
}

type governanceAuditMemoryDB struct {
	rows  map[string]governanceAuditMemoryRow
	execs []fakeExecCall
}

func newGovernanceAuditMemoryDB() *governanceAuditMemoryDB {
	return &governanceAuditMemoryDB{rows: map[string]governanceAuditMemoryRow{}}
}

func (db *governanceAuditMemoryDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execs = append(db.execs, fakeExecCall{query: query, args: args})
	if strings.Contains(query, "INSERT INTO governance_audit_events") {
		const columnsPerRow = 13
		for i := 0; i < len(args)/columnsPerRow; i++ {
			offset := i * columnsPerRow
			row := governanceAuditMemoryRow{
				eventID:            args[offset+0].(string),
				eventType:          args[offset+1].(string),
				actorClass:         args[offset+2].(string),
				actorIDHash:        governanceAuditStringArg(args[offset+3]),
				servicePrincipal:   governanceAuditStringArg(args[offset+4]),
				scopeClass:         args[offset+5].(string),
				scopeIDHash:        governanceAuditStringArg(args[offset+6]),
				decision:           args[offset+7].(string),
				reasonCode:         args[offset+8].(string),
				correlationID:      governanceAuditStringArg(args[offset+9]),
				policyRevisionHash: governanceAuditStringArg(args[offset+10]),
				occurredAt:         args[offset+11].(time.Time),
				persistedAt:        args[offset+12].(time.Time),
			}
			if _, ok := db.rows[row.eventID]; !ok {
				db.rows[row.eventID] = row
			}
		}
		return fakeResult{}, nil
	}
	if strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX") {
		return fakeResult{}, nil
	}
	return nil, sql.ErrNoRows
}

func (db *governanceAuditMemoryDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return &queueFakeRows{}, nil
}

func governanceAuditStringArg(value any) string {
	if value == nil {
		return ""
	}
	return value.(string)
}

func (db *governanceAuditMemoryDB) lastRow() governanceAuditMemoryRow {
	for _, row := range db.rows {
		return row
	}
	return governanceAuditMemoryRow{}
}
