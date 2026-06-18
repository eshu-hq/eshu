package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
)

const (
	functionSourceBatchSize = 500
	functionSourceColumns   = 5
)

// functionSourceSchemaSQL is the durable store for value-flow param-level taint
// sources (#2931). One row per (function_id, param_index): the parser-derived
// entry points (e.g. an *http.Request parameter) the interprocedural fixpoint
// needs as source ports, which summary.Effects does not carry. Keyed by the
// generation-independent FunctionID alongside its parameter index, with the
// owning repo recorded for filtering.
const functionSourceSchemaSQL = `
CREATE TABLE IF NOT EXISTS function_sources (
    function_id TEXT NOT NULL,
    param_index INTEGER NOT NULL,
    kind TEXT NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (function_id, param_index)
);
CREATE INDEX IF NOT EXISTS function_sources_repo_idx
    ON function_sources (repo, function_id);
`

const upsertFunctionSourceBatchPrefix = `
INSERT INTO function_sources (
    function_id, param_index, kind, repo, updated_at
) VALUES `

const upsertFunctionSourceBatchSuffix = `
ON CONFLICT (function_id, param_index) DO UPDATE
SET kind = EXCLUDED.kind,
    repo = EXCLUDED.repo,
    updated_at = EXCLUDED.updated_at
WHERE function_sources.updated_at <= EXCLUDED.updated_at
`

const loadFunctionSourcesSQL = `
SELECT function_id, param_index, kind
FROM function_sources
ORDER BY function_id ASC, param_index ASC
`

// FunctionSourceSchemaSQL returns the DDL for durable value-flow function sources.
func FunctionSourceSchemaSQL() string {
	return functionSourceSchemaSQL
}

func functionSourceBootstrapDefinition() Definition {
	return Definition{
		Name: "function_sources",
		Path: "schema/data-plane/postgres/029_function_sources.sql",
		SQL:  functionSourceSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, functionSourceBootstrapDefinition())
}

// functionIDRepo returns the repository component of a FunctionID
// (repo\x1fpkg\x1freceiver\x1fname), or the empty string when unset.
func functionIDRepo(functionID string) string {
	if idx := strings.Index(functionID, "\x1f"); idx >= 0 {
		return functionID[:idx]
	}
	return ""
}

// FunctionSourceStore persists value-flow param-level taint sources as interproc
// source ports for the cross-repo fixpoint.
type FunctionSourceStore struct {
	db ExecQueryer
}

// NewFunctionSourceStore constructs a Postgres-backed function source store.
func NewFunctionSourceStore(db ExecQueryer) FunctionSourceStore {
	return FunctionSourceStore{db: db}
}

// EnsureSchema applies the function source DDL.
func (s FunctionSourceStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("function source store database is required")
	}
	if _, err := s.db.ExecContext(ctx, functionSourceSchemaSQL); err != nil {
		return fmt.Errorf("ensure function source schema: %w", err)
	}
	return nil
}

// UpsertSources persists each param-level source, idempotent on
// (function_id, param_index). Safe for concurrent writers: racing writes for the
// same port converge on the last committed row.
func (s FunctionSourceStore) UpsertSources(ctx context.Context, sources []interproc.Source, updatedAt time.Time) error {
	if s.db == nil {
		return fmt.Errorf("function source store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("function source updated_at is required")
	}
	if len(sources) == 0 {
		return nil
	}
	for i := 0; i < len(sources); i += functionSourceBatchSize {
		end := i + functionSourceBatchSize
		if end > len(sources) {
			end = len(sources)
		}
		if err := s.upsertBatch(ctx, sources[i:end], updatedAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s FunctionSourceStore) upsertBatch(ctx context.Context, sources []interproc.Source, updatedAt time.Time) error {
	values := make([]string, 0, len(sources))
	args := make([]any, 0, len(sources)*functionSourceColumns)
	for _, src := range sources {
		functionID := strings.TrimSpace(string(src.Port.Func))
		if functionID == "" {
			return fmt.Errorf("function source id is required")
		}
		base := len(args)
		placeholders := make([]string, 0, functionSourceColumns)
		for i := 1; i <= functionSourceColumns; i++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+i))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args,
			functionID,
			src.Port.Slot.Index,
			src.Kind,
			functionIDRepo(functionID),
			updatedAt,
		)
	}
	query := upsertFunctionSourceBatchPrefix + strings.Join(values, ", ") + upsertFunctionSourceBatchSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert function sources: %w", err)
	}
	return nil
}

// LoadSources reloads every persisted source as an interproc source port, in
// deterministic order, so the fixpoint can compose them with the summaries.
func (s FunctionSourceStore) LoadSources(ctx context.Context) ([]interproc.Source, error) {
	if s.db == nil {
		return nil, fmt.Errorf("function source store database is required")
	}
	rows, err := s.db.QueryContext(ctx, loadFunctionSourcesSQL)
	if err != nil {
		return nil, fmt.Errorf("load function sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sources []interproc.Source
	for rows.Next() {
		var functionID, kind string
		var paramIndex int
		if err := rows.Scan(&functionID, &paramIndex, &kind); err != nil {
			return nil, fmt.Errorf("scan function source: %w", err)
		}
		sources = append(sources, interproc.Source{
			Port: interproc.Port{
				Func: interproc.FunctionID(functionID),
				Slot: interproc.Slot{Kind: interproc.SlotParam, Index: paramIndex},
			},
			Kind: kind,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load function sources: %w", err)
	}
	return sources, nil
}
