// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/lib/pq"
)

const (
	valueFlowFixpointComponentBatchSize = 250
	valueFlowFixpointComponentColumns   = 3
)

const valueFlowFixpointComponentSchemaSQL = `
CREATE TABLE IF NOT EXISTS value_flow_fixpoint_components (
    component_key TEXT PRIMARY KEY,
    result JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS value_flow_fixpoint_components_updated_idx
    ON value_flow_fixpoint_components (updated_at DESC, component_key);
`

const upsertValueFlowFixpointComponentPrefix = `
INSERT INTO value_flow_fixpoint_components (
    component_key,
    result,
    updated_at
) VALUES `

const upsertValueFlowFixpointComponentSuffix = `
ON CONFLICT (component_key) DO UPDATE
SET result = EXCLUDED.result,
    updated_at = EXCLUDED.updated_at
`

const loadValueFlowFixpointComponentsSQL = `
SELECT component_key, result
FROM value_flow_fixpoint_components
WHERE component_key = ANY($1)
ORDER BY component_key ASC
`

// ValueFlowFixpointComponentSchemaSQL returns the DDL for durable value-flow
// component solve results.
func ValueFlowFixpointComponentSchemaSQL() string {
	return valueFlowFixpointComponentSchemaSQL
}

func valueFlowFixpointComponentBootstrapDefinition() Definition {
	return Definition{
		Name: "value_flow_fixpoint_components",
		Path: "schema/data-plane/postgres/032_value_flow_fixpoint_components.sql",
		SQL:  valueFlowFixpointComponentSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, valueFlowFixpointComponentBootstrapDefinition())
}

// ValueFlowFixpointComponentStore persists solved value-flow weak-component
// results keyed by the reducer's content-derived component key.
type ValueFlowFixpointComponentStore struct {
	db ExecQueryer
}

// NewValueFlowFixpointComponentStore constructs a Postgres-backed component
// cache store.
func NewValueFlowFixpointComponentStore(db ExecQueryer) ValueFlowFixpointComponentStore {
	return ValueFlowFixpointComponentStore{db: db}
}

// EnsureSchema applies the value-flow component cache DDL.
func (s ValueFlowFixpointComponentStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("value-flow fixpoint component store database is required")
	}
	if _, err := s.db.ExecContext(ctx, valueFlowFixpointComponentSchemaSQL); err != nil {
		return fmt.Errorf("ensure value-flow fixpoint component schema: %w", err)
	}
	return nil
}

// StoreValueFlowFixpointComponents upserts solved component results
// idempotently. Racing reducers that compute the same component key converge on
// the same result payload.
func (s ValueFlowFixpointComponentStore) StoreValueFlowFixpointComponents(
	ctx context.Context,
	entries map[string]interproc.Result,
) error {
	if s.db == nil {
		return fmt.Errorf("value-flow fixpoint component store database is required")
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for i := 0; i < len(keys); i += valueFlowFixpointComponentBatchSize {
		end := i + valueFlowFixpointComponentBatchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := s.upsertBatch(ctx, keys[i:end], entries); err != nil {
			return err
		}
	}
	return nil
}

func (s ValueFlowFixpointComponentStore) upsertBatch(
	ctx context.Context,
	keys []string,
	entries map[string]interproc.Result,
) error {
	values := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*valueFlowFixpointComponentColumns)
	now := time.Now().UTC()
	for _, key := range keys {
		result, err := json.Marshal(entries[key])
		if err != nil {
			return fmt.Errorf("marshal value-flow fixpoint component %q: %w", key, err)
		}
		base := len(args)
		placeholders := make([]string, 0, valueFlowFixpointComponentColumns)
		for i := 1; i <= valueFlowFixpointComponentColumns; i++ {
			placeholders = append(placeholders, fmt.Sprintf("$%d", base+i))
		}
		values = append(values, "("+strings.Join(placeholders, ", ")+")")
		args = append(args, key, result, now)
	}
	if len(args) == 0 {
		return nil
	}
	query := upsertValueFlowFixpointComponentPrefix + strings.Join(values, ", ") + upsertValueFlowFixpointComponentSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("upsert value-flow fixpoint components: %w", err)
	}
	return nil
}

// LoadValueFlowFixpointComponents reloads the requested solved component
// results. Missing keys are omitted from the returned map.
func (s ValueFlowFixpointComponentStore) LoadValueFlowFixpointComponents(
	ctx context.Context,
	keys []string,
) (map[string]interproc.Result, error) {
	if s.db == nil {
		return nil, fmt.Errorf("value-flow fixpoint component store database is required")
	}
	keys = normalizeValueFlowFixpointComponentKeys(keys)
	if len(keys) == 0 {
		return map[string]interproc.Result{}, nil
	}
	rows, err := s.db.QueryContext(ctx, loadValueFlowFixpointComponentsSQL, pq.Array(keys))
	if err != nil {
		return nil, fmt.Errorf("load value-flow fixpoint components: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]interproc.Result)
	for rows.Next() {
		var key string
		var resultBytes []byte
		if err := rows.Scan(&key, &resultBytes); err != nil {
			return nil, fmt.Errorf("scan value-flow fixpoint component: %w", err)
		}
		var result interproc.Result
		if err := json.Unmarshal(resultBytes, &result); err != nil {
			return nil, fmt.Errorf("decode value-flow fixpoint component %q: %w", key, err)
		}
		out[key] = result
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load value-flow fixpoint components: %w", err)
	}
	return out, nil
}

func normalizeValueFlowFixpointComponentKeys(keys []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
