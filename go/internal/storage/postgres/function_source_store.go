package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

const functionSourceSchemaSQL = `
CREATE TABLE IF NOT EXISTS function_sources (
    function_id TEXT NOT NULL,
    param_index INTEGER NOT NULL,
    source_kind TEXT NOT NULL,
    source_label TEXT NOT NULL DEFAULT '',
    lang TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (function_id, param_index, source_kind, source_label)
);

CREATE INDEX IF NOT EXISTS function_sources_repo_idx
    ON function_sources (repo, function_id, param_index);
`

const upsertFunctionSourceSQL = `
INSERT INTO function_sources (
    function_id,
    param_index,
    source_kind,
    source_label,
    lang,
    repo,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (function_id, param_index, source_kind, source_label) DO UPDATE
SET lang = EXCLUDED.lang,
    repo = EXCLUDED.repo,
    updated_at = EXCLUDED.updated_at
WHERE function_sources.updated_at <= EXCLUDED.updated_at
`

const deleteFunctionSourcesSQL = `
DELETE FROM function_sources
WHERE function_id = $1
  AND updated_at <= $2
`

// FunctionSourceSchemaSQL returns the DDL for durable value-flow source entry
// points.
func FunctionSourceSchemaSQL() string {
	return functionSourceSchemaSQL
}

// FunctionSourceStore persists parser-emitted param-level source entry points
// for reducer value-flow Program assembly.
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

// UpsertSources persists source entry points with an idempotent stable key.
func (s FunctionSourceStore) UpsertSources(
	ctx context.Context,
	sources []collector.ValueFlowSourceSnapshot,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("function source store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("function source updated_at is required")
	}
	for _, source := range sources {
		repo, err := functionSummaryRepo(source.FunctionID)
		if err != nil {
			return err
		}
		if source.ParamIndex < 0 {
			return fmt.Errorf("function source param index is required for %q", source.FunctionID)
		}
		if strings.TrimSpace(source.Kind) == "" {
			return fmt.Errorf("function source kind is required for %q", source.FunctionID)
		}
		if _, err := s.db.ExecContext(
			ctx,
			upsertFunctionSourceSQL,
			string(source.FunctionID),
			source.ParamIndex,
			strings.TrimSpace(source.Kind),
			strings.TrimSpace(source.Label),
			strings.TrimSpace(source.Language),
			repo,
			updatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("upsert function source %q: %w", source.FunctionID, err)
		}
	}
	return nil
}

// ReplaceSourcesForFunctions replaces source entry points for the given
// functions, removing stale source rows before inserting the current rows.
func (s FunctionSourceStore) ReplaceSourcesForFunctions(
	ctx context.Context,
	functionIDs []summary.FunctionID,
	sources []collector.ValueFlowSourceSnapshot,
	updatedAt time.Time,
) error {
	if s.db == nil {
		return fmt.Errorf("function source store database is required")
	}
	if updatedAt.IsZero() {
		return fmt.Errorf("function source updated_at is required")
	}
	for _, id := range uniqueFunctionSourceIDs(functionIDs) {
		if _, err := functionSummaryRepo(id); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, deleteFunctionSourcesSQL, string(id), updatedAt.UTC()); err != nil {
			return fmt.Errorf("delete function sources for %q: %w", id, err)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	return s.UpsertSources(ctx, sources, updatedAt)
}

func uniqueFunctionSourceIDs(ids []summary.FunctionID) []summary.FunctionID {
	seen := make(map[summary.FunctionID]struct{}, len(ids))
	out := make([]summary.FunctionID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
