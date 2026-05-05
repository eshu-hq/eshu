package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestGenerationFreshnessCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scopeID      string
		generationID string
		db           *generationFreshnessTestDB
		wantCurrent  bool
		wantErr      bool
	}{
		{
			name:         "current when generation matches active",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			db: &generationFreshnessTestDB{
				scopes: map[string]sql.NullString{
					"scope-123": {String: "gen-abc", Valid: true},
				},
			},
			wantCurrent: true,
		},
		{
			name:         "stale when generation does not match active",
			scopeID:      "scope-123",
			generationID: "gen-old",
			db: &generationFreshnessTestDB{
				scopes: map[string]sql.NullString{
					"scope-123": {String: "gen-new", Valid: true},
				},
			},
			wantCurrent: false,
		},
		{
			name:         "current when scope not found",
			scopeID:      "scope-unknown",
			generationID: "gen-abc",
			db: &generationFreshnessTestDB{
				scopes: map[string]sql.NullString{},
			},
			wantCurrent: true,
		},
		{
			name:         "current when active_generation_id is NULL",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			db: &generationFreshnessTestDB{
				scopes: map[string]sql.NullString{
					"scope-123": {Valid: false},
				},
			},
			wantCurrent: true,
		},
		{
			name:         "error propagated from database",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			db: &generationFreshnessTestDB{
				queryErr: fmt.Errorf("connection refused"),
			},
			wantCurrent: false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := NewGenerationFreshnessCheck(tt.db)
			gotCurrent, err := check(context.Background(), tt.scopeID, tt.generationID)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotCurrent != tt.wantCurrent {
				t.Fatalf("current = %v, want %v", gotCurrent, tt.wantCurrent)
			}
		})
	}
}

func TestPriorGenerationCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scopeID      string
		generationID string
		db           *generationFreshnessTestDB
		wantPrior    bool
		wantErr      bool
	}{
		{
			name:         "false for first generation",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			db: &generationFreshnessTestDB{
				generations: map[string][]string{"scope-123": []string{"gen-abc"}},
			},
		},
		{
			name:         "true when another generation exists",
			scopeID:      "scope-123",
			generationID: "gen-new",
			db: &generationFreshnessTestDB{
				generations: map[string][]string{"scope-123": []string{"gen-old", "gen-new"}},
			},
			wantPrior: true,
		},
		{
			name:         "false when scope unknown",
			scopeID:      "scope-unknown",
			generationID: "gen-abc",
			db:           &generationFreshnessTestDB{},
		},
		{
			name:         "error propagated from database",
			scopeID:      "scope-123",
			generationID: "gen-abc",
			db: &generationFreshnessTestDB{
				queryErr: fmt.Errorf("connection refused"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			check := NewPriorGenerationCheck(tt.db)
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

// -- test helpers --

type generationFreshnessTestDB struct {
	scopes      map[string]sql.NullString // scope_id -> active_generation_id
	generations map[string][]string       // scope_id -> generation_ids
	queryErr    error
}

func (db *generationFreshnessTestDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("ExecContext not implemented in test stub")
}

func (db *generationFreshnessTestDB) QueryContext(_ context.Context, _ string, args ...any) (Rows, error) {
	if db.queryErr != nil {
		return nil, db.queryErr
	}

	switch len(args) {
	case 1:
		scopeID := args[0].(string)
		activeGen, found := db.scopes[scopeID]
		if !found {
			return &generationFreshnessTestRows{data: nil, idx: -1}, nil
		}
		return &generationFreshnessTestRows{
			data: [][]any{{activeGen}},
			idx:  -1,
		}, nil
	case 2:
		scopeID := args[0].(string)
		generationID := args[1].(string)
		exists := false
		for _, candidate := range db.generations[scopeID] {
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
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *generationFreshnessTestRows) Err() error   { return nil }
func (r *generationFreshnessTestRows) Close() error { return nil }
