// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type codeFlowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresCodeFlowStore reads active-generation code-flow evidence from
// fact_records.
type PostgresCodeFlowStore struct {
	db codeFlowQueryer
}

// NewPostgresCodeFlowStore constructs the Postgres code-flow read store.
func NewPostgresCodeFlowStore(db codeFlowQueryer) PostgresCodeFlowStore {
	return PostgresCodeFlowStore{db: db}
}

const listActiveCodeFlowFactsSQL = `
SELECT
    fact.fact_id,
    fact.generation_id,
    fact.fact_kind,
    fact.observed_at,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.fact_kind = ANY($1::text[])
  AND fact.payload->>'repo_id' = $2
  AND ($3::text = '' OR lower(coalesce(nullif(fact.payload->>'language', ''), nullif(fact.payload->>'lang', ''))) = $3)
  AND ($4::text = '' OR fact.payload->>'relative_path' = $4)
  AND (
    $5::text = ''
    OR fact.payload->>'function_name' = $5
    OR fact.payload->>'source_function_name' = $5
    OR fact.payload->>'sink_function_name' = $5
  )
  AND (
    $6::int = 0
    OR coalesce(nullif(fact.payload->>'line_number', ''), '0')::int = $6
    OR coalesce(nullif(fact.payload->>'source_line', ''), '0')::int = $6
    OR coalesce(nullif(fact.payload->>'sink_line', ''), '0')::int = $6
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $7
`

// ListCodeFlow loads bounded active-generation code-flow rows.
func (s PostgresCodeFlowStore) ListCodeFlow(ctx context.Context, filter CodeFlowFilter) (CodeFlowReadModel, error) {
	if s.db == nil {
		return CodeFlowReadModel{}, fmt.Errorf("code-flow store database is required")
	}
	kinds := codeFlowFactKinds(filter.Kind)
	if len(kinds) == 0 {
		return CodeFlowReadModel{Freshness: FreshnessFresh}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = codeFlowDefaultLimit + 1
	}
	rows, err := s.db.QueryContext(
		ctx,
		listActiveCodeFlowFactsSQL,
		kinds,
		strings.TrimSpace(filter.RepoID),
		normalizeCodeFlowLanguage(filter.Language),
		strings.TrimSpace(filter.FilePath),
		strings.TrimSpace(filter.Symbol),
		filter.Line,
		limit,
	)
	if err != nil {
		return CodeFlowReadModel{}, fmt.Errorf("list active code-flow facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	model := CodeFlowReadModel{Freshness: FreshnessFresh}
	for rows.Next() {
		var factID, generationID, factKind string
		var observedAt time.Time
		var rawPayload []byte
		if err := rows.Scan(&factID, &generationID, &factKind, &observedAt, &rawPayload); err != nil {
			return CodeFlowReadModel{}, fmt.Errorf("scan active code-flow fact: %w", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return CodeFlowReadModel{}, fmt.Errorf("decode active code-flow payload: %w", err)
		}
		switch factKind {
		case facts.CodeDataflowFunctionFactKind:
			model.Functions = append(model.Functions, codeFlowFunctionFromPayload(payload, factID, generationID, factKind, observedAt))
		case facts.CodeTaintEvidenceFactKind, facts.CodeInterprocEvidenceFactKind:
			model.TaintPaths = append(model.TaintPaths, codeFlowTaintPathFromPayload(payload, factID, generationID, factKind, observedAt))
		}
	}
	if err := rows.Err(); err != nil {
		return CodeFlowReadModel{}, fmt.Errorf("list active code-flow facts: %w", err)
	}
	return model, nil
}

func codeFlowFactKinds(kind CodeFlowKind) []string {
	switch kind {
	case CodeFlowKindTaintPath:
		return []string{facts.CodeTaintEvidenceFactKind, facts.CodeInterprocEvidenceFactKind}
	case CodeFlowKindReachingDef, CodeFlowKindCFGSummary, CodeFlowKindPDGSummary:
		return []string{facts.CodeDataflowFunctionFactKind}
	default:
		return nil
	}
}

func codeFlowFunctionFromPayload(
	payload map[string]any,
	factID string,
	generationID string,
	factKind string,
	observedAt time.Time,
) CodeFlowFunction {
	return CodeFlowFunction{
		RepoID:              StringVal(payload, "repo_id"),
		RelativePath:        StringVal(payload, "relative_path"),
		FunctionName:        StringVal(payload, "function_name"),
		FunctionUID:         StringVal(payload, "function_uid"),
		Language:            normalizeCodeFlowLanguage(StringVal(payload, "language")),
		LineNumber:          IntVal(payload, "line_number"),
		CFGBlocks:           anySliceVal(payload, "cfg_blocks"),
		CFGEdges:            anySliceVal(payload, "cfg_edges"),
		DefUse:              mapSliceVal(payload, "def_use"),
		ControlDependencies: mapSliceVal(payload, "control_dependencies"),
		Overflow:            BoolVal(payload, "overflow"),
		OverflowReason:      StringVal(payload, "overflow_reason"),
		EvidenceHandle:      codeFlowEvidenceHandle(factKind, factID),
		SourceGenerationID:  generationID,
		SourceObservedAt:    observedAt,
	}
}

func codeFlowTaintPathFromPayload(
	payload map[string]any,
	factID string,
	generationID string,
	factKind string,
	observedAt time.Time,
) CodeFlowTaintPath {
	functionName := StringVal(payload, "function_name")
	if functionName == "" {
		functionName = strings.TrimSpace(StringVal(payload, "source_function_name") + " -> " + StringVal(payload, "sink_function_name"))
	}
	return CodeFlowTaintPath{
		RepoID:             StringVal(payload, "repo_id"),
		RelativePath:       StringVal(payload, "relative_path"),
		FunctionName:       functionName,
		Language:           normalizeCodeFlowLanguage(StringVal(payload, "language")),
		SourceKind:         StringVal(payload, "source_kind"),
		SinkKind:           StringVal(payload, "sink_kind"),
		SourceLine:         IntVal(payload, "source_line"),
		SinkLine:           IntVal(payload, "sink_line"),
		Confidence:         floatVal(payload, "confidence"),
		EvidenceHandle:     codeFlowEvidenceHandle(factKind, factID),
		SourceGenerationID: generationID,
		SourceObservedAt:   observedAt,
	}
}

func codeFlowEvidenceHandle(factKind string, factID string) string {
	if factKind == "" || factID == "" {
		return ""
	}
	return "fact://" + factKind + "/" + factID
}

func anySliceVal(payload map[string]any, key string) []any {
	values, _ := payload[key].([]any)
	return values
}

func mapSliceVal(payload map[string]any, key string) []map[string]any {
	switch typed := payload[key].(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if ok {
				out = append(out, row)
			}
		}
		return out
	default:
		return nil
	}
}
