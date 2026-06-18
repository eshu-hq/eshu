package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

const (
	functionSummaryBatchSize = 500
	functionSummaryColumns   = 6
)

const functionSummarySchemaSQL = `
CREATE TABLE IF NOT EXISTS function_summaries (
    function_id TEXT PRIMARY KEY,
    effects JSONB NOT NULL,
    version TEXT NOT NULL,
    structural_hash TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS function_summaries_repo_idx
    ON function_summaries (repo, function_id);

CREATE INDEX IF NOT EXISTS function_summaries_updated_idx
    ON function_summaries (updated_at DESC, function_id);
`

const upsertFunctionSummaryBatchPrefix = `
INSERT INTO function_summaries (
    function_id,
    effects,
    version,
    structural_hash,
    repo,
    updated_at
) VALUES `

const upsertFunctionSummaryBatchSuffix = `
ON CONFLICT (function_id) DO UPDATE
SET effects = EXCLUDED.effects,
    version = EXCLUDED.version,
    structural_hash = EXCLUDED.structural_hash,
    repo = EXCLUDED.repo,
    updated_at = EXCLUDED.updated_at
WHERE function_summaries.updated_at <= EXCLUDED.updated_at
`

const loadFunctionSummariesSQL = `
SELECT function_id, effects, version
FROM function_summaries
ORDER BY function_id ASC
`

// FunctionSummarySchemaSQL returns the DDL for durable value-flow function
// summaries.
func FunctionSummarySchemaSQL() string {
	return functionSummarySchemaSQL
}

// FunctionSummaryStore persists value-flow summary snapshots across reducer
// runs. It stores only the summary package's durable Snapshot form; the
// in-memory summary.Store remains owned by internal/parser/summary.
type FunctionSummaryStore struct {
	db ExecQueryer
}

// NewFunctionSummaryStore constructs a Postgres-backed function summary store.
func NewFunctionSummaryStore(db ExecQueryer) FunctionSummaryStore {
	return FunctionSummaryStore{db: db}
}

// EnsureSchema applies the function summary DDL.
func (s FunctionSummaryStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("function summary store database is required")
	}
	if _, err := s.db.ExecContext(ctx, functionSummarySchemaSQL); err != nil {
		return fmt.Errorf("ensure function summary schema: %w", err)
	}
	return nil
}

// UpsertSnapshot persists every function in a durable summary snapshot with an
// idempotent function_id conflict key. The write is safe for concurrent writers:
// racing writes for the same function converge on the last committed snapshot
// row rather than inserting duplicates.
func (s FunctionSummaryStore) UpsertSnapshot(ctx context.Context, snap summary.Snapshot, updatedAt time.Time) error {
	if s.db == nil {
		return fmt.Errorf("function summary store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("function summary snapshot updated_at is required")
	}
	functions := snap.Functions
	if len(functions) == 0 {
		return nil
	}
	for i := 0; i < len(functions); i += functionSummaryBatchSize {
		end := i + functionSummaryBatchSize
		if end > len(functions) {
			end = len(functions)
		}
		if err := s.upsertBatch(ctx, functions[i:end], updatedAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

// LoadSnapshot reloads all persisted summaries in deterministic function_id
// order so summary.Load can rebuild the exact in-memory Store state.
func (s FunctionSummaryStore) LoadSnapshot(ctx context.Context) (summary.Snapshot, error) {
	if s.db == nil {
		return summary.Snapshot{}, fmt.Errorf("function summary store database is required")
	}
	rows, err := s.db.QueryContext(ctx, loadFunctionSummariesSQL)
	if err != nil {
		return summary.Snapshot{}, fmt.Errorf("load function summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snap summary.Snapshot
	for rows.Next() {
		fn, err := scanFunctionSummary(rows)
		if err != nil {
			return summary.Snapshot{}, err
		}
		snap.Functions = append(snap.Functions, fn)
	}
	if err := rows.Err(); err != nil {
		return summary.Snapshot{}, fmt.Errorf("load function summaries: %w", err)
	}
	return snap, nil
}

func (s FunctionSummaryStore) upsertBatch(ctx context.Context, functions []summary.SnapshotFunction, updatedAt time.Time) error {
	values := make([]string, 0, len(functions))
	args := make([]any, 0, len(functions)*functionSummaryColumns)
	for _, fn := range functions {
		if strings.TrimSpace(string(fn.ID)) == "" {
			return fmt.Errorf("function summary id is required")
		}
		if strings.TrimSpace(fn.Version) == "" {
			return fmt.Errorf("function summary version is required for %q", fn.ID)
		}
		repo := functionSummaryRepo(fn.ID)
		if repo == "" {
			return fmt.Errorf("function summary repo is required for %q", fn.ID)
		}
		effects, err := json.Marshal(fn.Effects)
		if err != nil {
			return fmt.Errorf("marshal function summary effects: %w", err)
		}
		base := len(args)
		placeholders := make([]string, 0, functionSummaryColumns)
		for i := 1; i <= functionSummaryColumns; i++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+i))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args,
			string(fn.ID),
			effects,
			fn.Version,
			summary.StructuralHash(fn.Effects),
			repo,
			updatedAt,
		)
	}
	query := upsertFunctionSummaryBatchPrefix + strings.Join(values, ", ") + upsertFunctionSummaryBatchSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert function summaries: %w", err)
	}
	return nil
}

func scanFunctionSummary(rows Rows) (summary.SnapshotFunction, error) {
	var id string
	var effectsBytes []byte
	var version string
	if err := rows.Scan(&id, &effectsBytes, &version); err != nil {
		return summary.SnapshotFunction{}, fmt.Errorf("scan function summary: %w", err)
	}
	var effects summary.Effects
	if err := json.Unmarshal(effectsBytes, &effects); err != nil {
		return summary.SnapshotFunction{}, fmt.Errorf("decode function summary effects: %w", err)
	}
	return summary.SnapshotFunction{
		ID:      summary.FunctionID(id),
		Effects: effects,
		Version: version,
	}, nil
}

func functionSummaryRepo(id summary.FunctionID) string {
	raw := string(id)
	if idx := strings.Index(raw, "\x1f"); idx >= 0 {
		return raw[:idx]
	}
	return ""
}
