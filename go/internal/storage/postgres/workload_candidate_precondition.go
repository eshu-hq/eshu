// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// workloadCandidatePreconditionQuery reports whether one scope generation
// holds the preconditions candidate extraction needs: a repository fact
// carrying graph identity joined to a file fact whose parsed payload holds a
// signal VALUE extraction reads (#9697, value-aware since #9986). It
// transfers a single boolean — no envelopes, no payloads — so the workload
// projection input loader can shed its bulk scope-local load when the
// generation may hold candidates and the resolution fence is closed.
//
// The predicate mirrors ExtractWorkloadCandidates (reducer/candidate_loader.go)
// as a superset: repository identity (graph_id and name), file-to-repo join
// on repo_id, a parsed_file_data object, and then one of
//
//   - the Jenkinsfile path convention (trimmed, case-folded relative_path),
//   - a non-empty list in an array-bucket key (k8s_resources, ArgoCD,
//     dockerfile_stages, pipeline_calls, compose, CloudFormation),
//   - a present, non-null, non-blank chart_type, or
//   - an artifact_type of docker_compose or github_actions_workflow.
//
// Parsers write every signal key on every file as JSON null or an empty list
// when the file holds no such evidence, so key existence alone was true for
// every file-bearing scope on QA. The language and path gates extraction adds
// to some signals are deliberately not repeated: dropping a gate only makes
// the probe answer true more often, never false for a generation that yields
// a candidate. A true answer for a candidate-free generation can defer it,
// non-counting, until its own relationship generation activates or its
// producer reaches a terminal state; it can never count, because the pre-gate
// returns nil on a counting verdict and the post-load gate decides on real
// candidates. The differential live test
// proves that direction against ExtractWorkloadCandidates on the same
// payloads. Tombstones are intentionally NOT filtered: the scope-local kind
// loads hand tombstoned envelopes to extraction, so the probe sees the same
// input.
//
// Every key travels in a bound text[] parameter derived from
// factload.WorkloadCandidate*Keys, so no key text is interpolated into the
// SQL (#9986: the original interpolation wrote '{'k','k2'}' and PostgreSQL
// rejected every execution with SQLSTATE 42601).
//
// Performance: two index-friendly equality partitions (scope_id,
// generation_id, fact_kind) and an EXISTS that stops at the first joined
// pair. The file side reads repo_id, relative_path, and parsed_file_data
// through ONE jsonb_to_record call: each separate payload->>'key' on a
// TOASTed payload decompresses it again, which tripled the per-row cost on a
// 300,000-file scope. jsonb_to_record needs an object payload, which every
// file fact has by construction (the envelope payload is a map). A false
// answer must test every file of the generation, so its cost is linear in
// the file count (docs/internal/evidence/9986-workload-probe-plan.md).
func workloadCandidatePreconditionQuery() string {
	return workloadCandidatePreconditionSQL
}

// probeGoSpace matches one character strings.TrimSpace removes
// (unicode.IsSpace), so the blank and token tests agree with extraction on
// padded values.
const probeGoSpace = `[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]`

// probeGoTrimPattern removes the leading and trailing characters
// strings.TrimSpace removes. Extraction compares the trimmed repo_id of a file
// fact with the trimmed graph_id of the repository fact, so the join accepts
// the trimmed comparison too; the exact comparison stays first so the common
// case never runs the regular expression.
const probeGoTrimPattern = `'^` + probeGoSpace + `+|` + probeGoSpace + `+$'`

// workloadCandidatePreconditionSQL is the probe statement: $1 scope, $2
// generation, $3 every signal key (cheap key-existence prefilter), $4 the
// array-bucket keys, $5 the presence keys, $6 the token keys (see
// workloadCandidatePreconditionQuery).
const workloadCandidatePreconditionSQL = `SELECT EXISTS (
  SELECT 1
  FROM fact_records AS repo
  WHERE repo.scope_id = $1
    AND repo.generation_id = $2
    AND repo.fact_kind = 'repository'
    AND repo.payload->>'graph_id' <> ''
    AND repo.payload->>'name' <> ''
    AND EXISTS (
      SELECT 1
      FROM fact_records AS f
      CROSS JOIN LATERAL jsonb_to_record(f.payload) AS x(repo_id text, relative_path text, parsed_file_data jsonb)
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND (
          x.repo_id = repo.payload->>'graph_id'
          OR regexp_replace(x.repo_id, ` + probeGoTrimPattern + `, '', 'g')
           = regexp_replace(repo.payload->>'graph_id', ` + probeGoTrimPattern + `, '', 'g')
        )
        AND jsonb_typeof(x.parsed_file_data) = 'object'
        AND (
          x.relative_path ~ '^` + probeGoSpace + `*[jJ][eE][nN][kK\u212a][iI][nN][sS\u017f][fF][iI][lL][eE]` + probeGoSpace + `*$'
          OR (
            x.parsed_file_data ?| $3::text[]
            AND (
              EXISTS (
                SELECT 1 FROM unnest($4::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) = 'array' AND x.parsed_file_data->k.key <> '[]'::jsonb
              )
              OR EXISTS (
                SELECT 1 FROM unnest($5::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) <> 'null'
                  AND NOT (
                    jsonb_typeof(x.parsed_file_data->k.key) = 'string'
                    AND x.parsed_file_data->>k.key ~ '^` + probeGoSpace + `*(<nil>)?` + probeGoSpace + `*$'
                  )
              )
              OR EXISTS (
                SELECT 1 FROM unnest($6::text[]) AS k(key)
                WHERE jsonb_typeof(x.parsed_file_data->k.key) = 'string'
                  AND x.parsed_file_data->>k.key ~ '^` + probeGoSpace + `*(docker_compose|github_actions_workflow)` + probeGoSpace + `*$'
              )
            )
          )
        )
    )
)`

// ScopeMayHoldWorkloadCandidates implements
// factload.WorkloadCandidateProbe for the workload projection input
// loader's bulk-load pre-gate (#9697).
func (s FactStore) ScopeMayHoldWorkloadCandidates(
	ctx context.Context,
	scopeID string,
	generationID string,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("fact store database is required")
	}

	rows, err := s.database.QueryContext(
		ctx,
		workloadCandidatePreconditionQuery(),
		scopeID,
		generationID,
		array.StringArray(factload.WorkloadCandidateSignalKeys),
		array.StringArray(factload.WorkloadCandidateArrayBucketKeys),
		array.StringArray(factload.WorkloadCandidatePresenceKeys),
		array.StringArray(factload.WorkloadCandidateTokenKeys),
	)
	if err != nil {
		return false, fmt.Errorf("probe workload candidate precondition: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, fmt.Errorf("probe workload candidate precondition: no rows returned")
	}
	var mayHold bool
	if err := rows.Scan(&mayHold); err != nil {
		return false, fmt.Errorf("probe workload candidate precondition scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("probe workload candidate precondition rows: %w", err)
	}
	return mayHold, nil
}
