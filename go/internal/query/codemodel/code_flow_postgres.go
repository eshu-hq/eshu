// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeFlowQueryer is the minimal database port the Postgres flow reader
// needs. It is exported because the root constructor forwarder and the
// cmd wiring pass a live *sql.DB through it across the boundary.
type CodeFlowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// normalizeCodeFlowLanguage is a family-local copy of root's code_flow.go
// helper of the same name. The Postgres reader normalizes the language
// filter the same way the staying request normalization does, and that
// staying helper cannot cross the package boundary, so the leaf carries
// this byte-identical copy instead of importing root. Keep it
// behavior-identical to its root source.
func normalizeCodeFlowLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch language {
	case "c#", "cs":
		return "csharp"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	default:
		return language
	}
}

// floatVal is a family-local copy of root's compare.go helper of the same
// name. The flow row decoder reads confidence payloads through it, and the
// staying comparators that share it cannot cross the package boundary, so
// the leaf carries this byte-identical copy instead of importing root.
// Keep it behavior-identical to its root source.
func floatVal(row map[string]any, key string) float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0.0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0.0
	}
}

// PostgresCodeFlowStore reads cumulative active code-flow evidence from
// fact_records, preserving unchanged facts across delta generations.
type PostgresCodeFlowStore struct {
	db CodeFlowQueryer
}

// NewPostgresCodeFlowStore constructs the Postgres code-flow read store.
func NewPostgresCodeFlowStore(db CodeFlowQueryer) PostgresCodeFlowStore {
	return PostgresCodeFlowStore{db: db}
}

// ListActiveCodeFlowFactsSQL is the active-generation flow read. It is
// exported because the staying flow SQL tests assert on its text via the
// root forward.
const ListActiveCodeFlowFactsSQL = `
WITH active_scope AS (
    SELECT
        scope.scope_id,
        scope.active_generation_id,
        active_generation.ingested_at AS active_ingested_at
    FROM ingestion_scopes AS scope
    JOIN scope_generations AS active_generation
      ON active_generation.scope_id = scope.scope_id
     AND active_generation.generation_id = scope.active_generation_id
    WHERE active_generation.status = 'active'
),
ranked_candidates AS (
    SELECT
        fact.fact_id,
        fact.generation_id,
        fact.fact_kind,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload,
        ROW_NUMBER() OVER (
            PARTITION BY fact.scope_id, fact.stable_fact_key
            ORDER BY generation.ingested_at DESC, generation.generation_id DESC, fact.observed_at DESC, fact.fact_id DESC
        ) AS rn
    FROM fact_records AS fact
    JOIN active_scope
      ON active_scope.scope_id = fact.scope_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = ANY($1::text[])
      -- Literal code-flow kind conjunct (redundant with the $1 filter, whose
      -- values CodeFlowFactKinds always draws from this same set) so the
      -- planner can prove the fact_records_code_flow_repo_idx partial-index
      -- predicate at plan time. Without it a generic prepared plan cannot
      -- prove $1 is limited to these kinds and silently falls back to the
      -- all-scope over-fetch (#5280 review). Keep both: $1 selects the exact
      -- kind subset per read, this conjunct unlocks the partial index.
      AND fact.fact_kind IN ('code_taint_evidence', 'code_interproc_evidence', 'code_dataflow_function')
      AND fact.payload->>'repo_id' = $2
      AND generation.status IN ('active', 'superseded')
      AND generation.ingested_at <= active_scope.active_ingested_at
)
SELECT
    candidate.fact_id,
    candidate.generation_id,
    candidate.fact_kind,
    candidate.observed_at,
    candidate.payload
FROM ranked_candidates AS candidate
WHERE candidate.rn = 1
  AND candidate.is_tombstone = FALSE
  AND ($3::text = '' OR lower(coalesce(nullif(candidate.payload->>'language', ''), nullif(candidate.payload->>'lang', ''))) = $3)
  AND ($4::text = '' OR candidate.payload->>'relative_path' = $4)
  AND (
    $5::text = ''
    OR candidate.payload->>'function_name' = $5
    OR candidate.payload->>'source_function_name' = $5
    OR candidate.payload->>'sink_function_name' = $5
  )
  AND (
    $6::int = 0
    OR coalesce(nullif(candidate.payload->>'line_number', ''), '0')::int = $6
    OR coalesce(nullif(candidate.payload->>'source_line', ''), '0')::int = $6
    OR coalesce(nullif(candidate.payload->>'sink_line', ''), '0')::int = $6
  )
ORDER BY CASE candidate.fact_kind
             WHEN 'code_taint_evidence' THEN 0
             WHEN 'code_interproc_evidence' THEN 1
             ELSE 2
         END,
         candidate.observed_at ASC,
         candidate.fact_id ASC
LIMIT $7
`

// ListCodeFlow loads bounded cumulative active code-flow rows.
func (s PostgresCodeFlowStore) ListCodeFlow(ctx context.Context, filter CodeFlowFilter) (CodeFlowReadModel, error) {
	if s.db == nil {
		return CodeFlowReadModel{}, fmt.Errorf("code-flow store database is required")
	}
	kinds := CodeFlowFactKinds(filter.Kind)
	if len(kinds) == 0 {
		return CodeFlowReadModel{Freshness: querycontract.FreshnessFresh}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = CodeFlowDefaultLimit + 1
	}
	rows, err := s.db.QueryContext(
		ctx,
		ListActiveCodeFlowFactsSQL,
		kinds,
		strings.TrimSpace(filter.RepoID),
		normalizeCodeFlowLanguage(filter.Language),
		strings.TrimSpace(filter.FilePath),
		strings.TrimSpace(filter.Symbol),
		filter.Line,
		limit,
	)
	if err != nil {
		return CodeFlowReadModel{}, fmt.Errorf("list cumulative active code-flow facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	model := CodeFlowReadModel{Freshness: querycontract.FreshnessFresh}
	for rows.Next() {
		var factID, generationID, factKind string
		var observedAt time.Time
		var rawPayload []byte
		if err := rows.Scan(&factID, &generationID, &factKind, &observedAt, &rawPayload); err != nil {
			return CodeFlowReadModel{}, fmt.Errorf("scan cumulative active code-flow fact: %w", err)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return CodeFlowReadModel{}, fmt.Errorf("decode cumulative active code-flow payload: %w", err)
		}
		switch factKind {
		case facts.CodeDataflowFunctionFactKind:
			model.Functions = append(model.Functions, CodeFlowFunctionFromPayload(payload, factID, generationID, factKind, observedAt))
		case facts.CodeTaintEvidenceFactKind, facts.CodeInterprocEvidenceFactKind:
			model.TaintPaths = append(model.TaintPaths, codeFlowTaintPathFromPayload(payload, factID, generationID, factKind, observedAt))
		}
	}
	if err := rows.Err(); err != nil {
		return CodeFlowReadModel{}, fmt.Errorf("list cumulative active code-flow facts: %w", err)
	}
	return model, nil
}

// CodeFlowKindFactKinds maps each queryable CodeFlowKind to the fact kinds its
// read selects. It is the single dispatch source for CodeFlowFactKinds AND the
// enumeration the lockstep coverage guard ranges
// (TestCodeFlowSQLKeepsLiteralKindConjunctForPartialIndex), so a new CodeFlowKind
// wired here is automatically checked against facts.CodeFlowReadFactKinds() —
// there is no separate hand-maintained kind list a new entry could drift from.
// Every fact kind listed here MUST be covered by facts.CodeFlowReadFactKinds()
// (and therefore by the read's literal `fact_kind IN (...)` conjunct and the
// fact_records_code_flow_repo_idx partial index), or the read would select a
// kind the literal conjunct silently excludes, returning zero rows for it.
// CodeFlowKindFactKinds maps each queryable CodeFlowKind to its fact
// kinds. It is exported because the staying flow tests range over it via
// the root forward (same map).
var CodeFlowKindFactKinds = map[CodeFlowKind][]string{
	CodeFlowKindTaintPath:   {facts.CodeTaintEvidenceFactKind, facts.CodeInterprocEvidenceFactKind},
	CodeFlowKindReachingDef: {facts.CodeDataflowFunctionFactKind},
	CodeFlowKindCFGSummary:  {facts.CodeDataflowFunctionFactKind},
	CodeFlowKindPDGSummary:  {facts.CodeDataflowFunctionFactKind},
}

// CodeFlowFactKinds returns the fact kinds the read selects for kind, or nil for
// an unknown kind (ListCodeFlow short-circuits on nil). It returns a fresh copy
// of the canonical per-kind entry, never the shared map slice, so a caller that
// sorts or appends in place cannot corrupt CodeFlowKindFactKinds for later reads
// across all endpoints.
func CodeFlowFactKinds(kind CodeFlowKind) []string {
	entry := CodeFlowKindFactKinds[kind]
	if entry == nil {
		return nil
	}
	return append([]string(nil), entry...)
}

// CodeFlowFunctionFromPayload decodes one parser dataflow function record.
func CodeFlowFunctionFromPayload(
	payload map[string]any,
	factID string,
	generationID string,
	factKind string,
	observedAt time.Time,
) CodeFlowFunction {
	return CodeFlowFunction{
		RepoID:              querycontract.StringVal(payload, "repo_id"),
		RelativePath:        querycontract.StringVal(payload, "relative_path"),
		FunctionName:        querycontract.StringVal(payload, "function_name"),
		FunctionUID:         querycontract.StringVal(payload, "function_uid"),
		Language:            normalizeCodeFlowLanguage(querycontract.StringVal(payload, "language")),
		LineNumber:          querycontract.IntVal(payload, "line_number"),
		CFGBlocks:           anySliceVal(payload, "cfg_blocks"),
		CFGEdges:            anySliceVal(payload, "cfg_edges"),
		DefUse:              mapSliceVal(payload, "def_use"),
		ControlDependencies: mapSliceVal(payload, "control_dependencies"),
		Overflow:            querycontract.BoolVal(payload, "overflow"),
		OverflowReason:      querycontract.StringVal(payload, "overflow_reason"),
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
	functionName := querycontract.StringVal(payload, "function_name")
	if functionName == "" {
		functionName = strings.TrimSpace(querycontract.StringVal(payload, "source_function_name") + " -> " + querycontract.StringVal(payload, "sink_function_name"))
	}
	return CodeFlowTaintPath{
		RepoID:             querycontract.StringVal(payload, "repo_id"),
		RelativePath:       querycontract.StringVal(payload, "relative_path"),
		FunctionName:       functionName,
		Language:           normalizeCodeFlowLanguage(querycontract.StringVal(payload, "language")),
		SourceKind:         querycontract.StringVal(payload, "source_kind"),
		SinkKind:           querycontract.StringVal(payload, "sink_kind"),
		SourceLine:         querycontract.IntVal(payload, "source_line"),
		SinkLine:           querycontract.IntVal(payload, "sink_line"),
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

// The code-flow store contract split here from root code_flow.go (#6060
// lane A L1): the Postgres reader above implements ListCodeFlow over
// these values, and the staying handlers construct the filter. The kind,
// filter, read-model, and row types keep their exported names so the
// staying handlers, payload builders, and tests are unchanged through the
// root aliases; the limit bounds are exported because the staying request
// normalization clamps through them via the root forwards. The
// *CodeHandler route methods and the CodeFlowStore port stay in root.
const (
	// CodeFlowDefaultLimit bounds one code-flow page when the caller names
	// no limit.
	CodeFlowDefaultLimit = 25
	// CodeFlowMaxLimit caps one code-flow page.
	CodeFlowMaxLimit = 100
)

// CodeFlowKind selects one bounded code-flow read surface.
type CodeFlowKind string

const (
	CodeFlowKindTaintPath   CodeFlowKind = "taint_path"
	CodeFlowKindReachingDef CodeFlowKind = "reaching_def"
	CodeFlowKindCFGSummary  CodeFlowKind = "cfg_summary"
	CodeFlowKindPDGSummary  CodeFlowKind = "pdg_summary"
)

// CodeFlowFilter is the scoped query contract passed to the code-flow store.
type CodeFlowFilter struct {
	Kind     CodeFlowKind
	RepoID   string
	Language string
	Symbol   string
	FilePath string
	Line     int
	Limit    int
}

// CodeFlowReadModel is the store-neutral active-generation code-flow snapshot.
type CodeFlowReadModel struct {
	Functions       []CodeFlowFunction
	TaintPaths      []CodeFlowTaintPath
	Freshness       querycontract.FreshnessState
	FreshnessDetail string
}

// CodeFlowFunction is one exact parser dataflow record for a function.
type CodeFlowFunction struct {
	RepoID              string
	RelativePath        string
	FunctionName        string
	FunctionUID         string
	Language            string
	LineNumber          int
	CFGBlocks           []any
	CFGEdges            []any
	DefUse              []map[string]any
	ControlDependencies []map[string]any
	Overflow            bool
	OverflowReason      string
	EvidenceHandle      string
	SourceGenerationID  string
	SourceObservedAt    time.Time
}

// CodeFlowTaintPath is one reducer-owned taint evidence path.
type CodeFlowTaintPath struct {
	RepoID             string
	RelativePath       string
	FunctionName       string
	Language           string
	SourceKind         string
	SinkKind           string
	SourceLine         int
	SinkLine           int
	Confidence         float64
	EvidenceHandle     string
	SourceGenerationID string
	SourceObservedAt   time.Time
}
