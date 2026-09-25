// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestGenerationFreshnessCheck(t *testing.T) {
	t.Parallel()

	row := func(active, status string, newer bool) freshnessRow {
		r := freshnessRow{intentIsNewer: newer}
		if active != "" {
			r.active = sql.NullString{String: active, Valid: true}
		}
		if status != "" {
			r.intentStatus = sql.NullString{String: status, Valid: true}
		}
		return r
	}

	tests := []struct {
		name          string
		scopeID       string
		generationID  string
		database      *generationFreshnessTestDB
		wantCurrent   bool
		wantRetryable bool
		wantErr       bool
	}{
		{
			name:         "current when generation matches active",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("gen-abc", "active", false),
			}},
			wantCurrent: true,
		},
		{
			name:         "older superseded generation stays terminal",
			scopeID:      "scope-123",
			generationID: "gen-old",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("gen-new", "superseded", false),
			}},
		},
		{
			name:         "pending but older than active is superseded, not deferred",
			scopeID:      "scope-123",
			generationID: "gen-old",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("gen-new", "pending", false),
			}},
		},
		{
			name:         "newer failed generation is superseded, not deferred",
			scopeID:      "scope-123",
			generationID: "gen-next",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("gen-new", "failed", true),
			}},
		},
		{
			name:         "missing generation row is superseded",
			scopeID:      "scope-123",
			generationID: "gen-missing",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("gen-new", "", false),
			}},
		},
		{
			name:         "newer pending generation defers with a retryable error",
			scopeID:      "scope-123",
			generationID: "gen-next",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("gen-new", "pending", true),
			}},
			wantRetryable: true,
		},
		{
			name:         "current when scope not found",
			scopeID:      "scope-unknown",
			generationID: "gen-abc",
			database:     &generationFreshnessTestDB{freshness: map[string]freshnessRow{}},
			wantCurrent:  true,
		},
		{
			name:         "current when active_generation_id is NULL",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			database: &generationFreshnessTestDB{freshness: map[string]freshnessRow{
				"scope-123": row("", "pending", false),
			}},
			wantCurrent: true,
		},
		{
			name:         "error propagated from database",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			database: &generationFreshnessTestDB{
				queryErr: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := NewGenerationFreshnessCheck(tt.database)
			gotCurrent, err := check(context.Background(), tt.scopeID, tt.generationID)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if reducer.IsRetryable(err) {
					t.Fatalf("lookup failure %v must not classify as a retryable deferral", err)
				}
				return
			}
			if gotCurrent != tt.wantCurrent {
				t.Fatalf("current = %v, want %v", gotCurrent, tt.wantCurrent)
			}
			if !tt.wantRetryable {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			assertGenerationNotYetActive(t, err, tt.scopeID, tt.generationID)
			// The runtime wraps the check error with %w before WorkSink.Fail;
			// the classification must survive that wrapping.
			assertGenerationNotYetActive(t, fmt.Errorf("generation freshness check: %w", err), tt.scopeID, tt.generationID)
		})
	}
}

func assertGenerationNotYetActive(t *testing.T, err error, scopeID, generationID string) {
	t.Helper()
	if !reducer.IsRetryable(err) {
		t.Fatalf("error %v is not retryable", err)
	}
	if !errors.Is(err, reducercontract.ErrGenerationNotYetActive) {
		t.Fatalf("error %v does not unwrap to ErrGenerationNotYetActive", err)
	}
	var classed interface{ FailureClass() string }
	if !errors.As(err, &classed) || classed.FailureClass() != reducercontract.GenerationActivationNotReadyFailureClass {
		t.Fatalf("error %v does not self-classify as %q", err, reducercontract.GenerationActivationNotReadyFailureClass)
	}
	var typed reducercontract.GenerationNotYetActiveError
	if !errors.As(err, &typed) || typed.ScopeID != scopeID || typed.GenerationID != generationID {
		t.Fatalf("error %#v does not carry scope %q generation %q", err, scopeID, generationID)
	}
}

// TestGenerationFreshnessCheckReadsOneSnapshot pins the check to one
// statement over primary-key lookups, ordered like the projector
// supersession SQL, so the active pointer and the intent generation status
// cannot come from different snapshots.
func TestGenerationFreshnessCheckReadsOneSnapshot(t *testing.T) {
	t.Parallel()

	database := &generationFreshnessTestDB{freshness: map[string]freshnessRow{}}
	if _, err := NewGenerationFreshnessCheck(database)(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("check error = %v", err)
	}
	if got := len(database.queries); got != 1 {
		t.Fatalf("queries = %d, want 1 statement", got)
	}
	for _, want := range []string{
		"FROM ingestion_scopes AS scope",
		"intent_generation.generation_id = $2",
		"active_generation.generation_id = scope.active_generation_id",
		"(intent_generation.ingested_at, intent_generation.generation_id)",
		"> (active_generation.ingested_at, active_generation.generation_id)",
		"WHERE scope.scope_id = $1",
	} {
		if !strings.Contains(database.queries[0], want) {
			t.Fatalf("freshness query missing %q:\n%s", want, database.queries[0])
		}
	}
}

func TestPriorGenerationCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scopeID      string
		generationID string
		database     *generationFreshnessTestDB
		wantPrior    bool
		wantErr      bool
	}{
		{
			name:         "false for first generation",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			database: &generationFreshnessTestDB{
				generations: map[string][]string{"scope-123": {"gen-abc"}},
			},
		},
		{
			name:         "true when another generation exists",
			scopeID:      "scope-123",
			generationID: "gen-new",
			database: &generationFreshnessTestDB{
				generations: map[string][]string{"scope-123": {"gen-old", "gen-new"}},
			},
			wantPrior: true,
		},
		{
			name:         "false when scope unknown",
			scopeID:      "scope-unknown",
			generationID: "gen-abc",
			database:     &generationFreshnessTestDB{},
		},
		{
			name:         "error propagated from database",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			database: &generationFreshnessTestDB{
				queryErr: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := NewPriorGenerationCheck(tt.database)
			gotPrior, err := check(context.Background(), tt.scopeID, tt.generationID)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotPrior != tt.wantPrior {
				t.Fatalf("prior = %v, want %v", gotPrior, tt.wantPrior)
			}
		})
	}
}

func TestIngestionStoreCurrentScopeGeneration(t *testing.T) {
	t.Parallel()

	database := &generationFreshnessTestDB{
		currentGenerations: map[string]currentGenerationRow{
			"scope-123": {generationID: "gen-abc", freshnessHint: "fingerprint-abc"},
		},
	}
	store := NewIngestionStore(database)
	current, found, err := store.CurrentScopeGeneration(context.Background(), "scope-123")
	if err != nil {
		t.Fatalf("CurrentScopeGeneration() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("CurrentScopeGeneration() found = false, want true")
	}
	if current.GenerationID != "gen-abc" || current.FreshnessHint != "fingerprint-abc" {
		t.Fatalf("CurrentScopeGeneration() = %#v, want gen-abc/fingerprint-abc", current)
	}
}

func TestIngestionStoreCurrentScopeGenerationReturnsNotFound(t *testing.T) {
	t.Parallel()

	store := NewIngestionStore(&generationFreshnessTestDB{})
	current, found, err := store.CurrentScopeGeneration(context.Background(), "scope-unknown")
	if err != nil {
		t.Fatalf("CurrentScopeGeneration() error = %v, want nil", err)
	}
	if found {
		t.Fatalf("CurrentScopeGeneration() found = true with current %#v, want false", current)
	}
}

// -- test helpers --

type currentGenerationRow struct {
	generationID  string
	freshnessHint string
}

// freshnessRow is one generationFreshnessSQL result row.
type freshnessRow struct {
	active        sql.NullString
	intentStatus  sql.NullString
	intentIsNewer bool
}

type generationFreshnessTestDB struct {
	freshness          map[string]freshnessRow // scope_id -> freshness row
	generations        map[string][]string     // scope_id -> generation_ids
	currentGenerations map[string]currentGenerationRow
	queryErr           error
	queries            []string
}

func (database *generationFreshnessTestDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (database *generationFreshnessTestDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	database.queries = append(database.queries, query)
	if database.queryErr != nil {
		return nil, database.queryErr
	}

	if query == generationFreshnessSQL {
		freshness, found := database.freshness[args[0].(string)]
		if !found {
			return &generationFreshnessTestRows{data: nil, idx: -1}, nil
		}
		return &generationFreshnessTestRows{
			data: [][]any{{freshness.active, freshness.intentStatus, freshness.intentIsNewer}},
			idx:  -1,
		}, nil
	}

	switch len(args) {
	case 1:
		scopeID := args[0].(string)
		if current, ok := database.currentGenerations[scopeID]; ok {
			return &generationFreshnessTestRows{
				data: [][]any{{current.generationID, current.freshnessHint}},
				idx:  -1,
			}, nil
		}
		return &generationFreshnessTestRows{data: nil, idx: -1}, nil
	case 2:
		scopeID := args[0].(string)
		generationID := args[1].(string)
		exists := false
		for _, candidate := range database.generations[scopeID] {
			if candidate != generationID {
				exists = true
				break
			}
		}
		return &generationFreshnessTestRows{
			data: [][]any{{exists}},
			idx:  -1,
		}, nil
	default:
		return nil, fmt.Errorf("expected 1 or 2 args, got %d", len(args))
	}
}

type generationFreshnessTestRows struct {
	data [][]any
	idx  int
}

func (r *generationFreshnessTestRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *generationFreshnessTestRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return sql.ErrNoRows
	}
	if len(dest) != len(r.data[r.idx]) {
		return fmt.Errorf("scan: got %d dest, want %d", len(dest), len(r.data[r.idx]))
	}
	for i, value := range r.data[r.idx] {
		switch d := dest[i].(type) {
		case *sql.NullString:
			v, ok := value.(sql.NullString)
			if !ok {
				return fmt.Errorf("scan value %d type = %T, want sql.NullString", i, value)
			}
			*d = v
		case *bool:
			v, ok := value.(bool)
			if !ok {
				return fmt.Errorf("scan value %d type = %T, want bool", i, value)
			}
			*d = v
		case *string:
			v, ok := value.(string)
			if !ok {
				return fmt.Errorf("scan value %d type = %T, want string", i, value)
			}
			*d = v
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *generationFreshnessTestRows) Err() error   { return nil }
func (r *generationFreshnessTestRows) Close() error { return nil }
