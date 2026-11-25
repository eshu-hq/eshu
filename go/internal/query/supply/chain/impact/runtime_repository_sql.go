// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// supplyChainRuntimeRepositoryDecoderJoin renders the canonical SQL decoder
// for a runtime fact's repository anchor. Its precedence must match
// supplyChainRuntimeContextRepositoryID: direct payload repository, payload
// scope or (only when payload scope is blank) envelope scope, then the first
// repository-like related scope, then the raw selected scope.
//
// Expressions and aliases are internal SQL identifiers selected by callers,
// never request values. Keeping this decoder shared makes filter membership
// and response hydration authorize and fold the same canonical repository.
func supplyChainRuntimeRepositoryDecoderJoin(
	payloadExpr string,
	scopeExpr string,
	alias string,
) string {
	return fmt.Sprintf(`
LEFT JOIN LATERAL (
  SELECT COALESCE(
           NULLIF(BTRIM(
             CASE
               WHEN jsonb_typeof(%[1]s->'repository_id') = 'string'
                 THEN %[1]s->>'repository_id'
             END
           ), ''),
           NULLIF(BTRIM(
             CASE
               WHEN jsonb_typeof(%[1]s->'repo_id') = 'string'
                 THEN %[1]s->>'repo_id'
             END
           ), ''),
           CASE
             WHEN selected_scope.value LIKE 'repository:%%'
               THEN selected_scope.value
             WHEN selected_scope.value LIKE 'git-repository-scope:%%'
               THEN NULLIF(BTRIM(SUBSTRING(selected_scope.value FROM 22)), '')
           END,
           (
             SELECT decoded_related.repository_id
             FROM (
               SELECT related.ordinal,
                      CASE
                        WHEN BTRIM(related.value) LIKE 'repository:%%'
                          THEN BTRIM(related.value)
                        WHEN BTRIM(related.value) LIKE 'git-repository-scope:%%'
                          THEN NULLIF(BTRIM(SUBSTRING(related.value FROM 22)), '')
                      END AS repository_id
               FROM jsonb_array_elements_text(
                 CASE
                   WHEN jsonb_typeof(%[1]s->'related_scope_ids') = 'array'
                     THEN %[1]s->'related_scope_ids'
                   WHEN jsonb_typeof(%[1]s->'related_scope_ids') = 'string'
                     THEN jsonb_build_array(%[1]s->'related_scope_ids')
                   ELSE '[]'::jsonb
                 END
               ) WITH ORDINALITY AS related(value, ordinal)
             ) AS decoded_related
             WHERE COALESCE(decoded_related.repository_id, '') <> ''
             ORDER BY decoded_related.ordinal
             LIMIT 1
           ),
           selected_scope.value
         ) AS repository_id
  FROM (
    SELECT COALESCE(
             NULLIF(BTRIM(
               CASE
                 WHEN jsonb_typeof(%[1]s->'scope_id') = 'string'
                   THEN %[1]s->>'scope_id'
               END
             ), ''),
             NULLIF(BTRIM(%[2]s), '')
           ) AS value
  ) AS selected_scope
) AS %[3]s ON TRUE`,
		payloadExpr,
		scopeExpr,
		alias,
	)
}

// selectSupplyChainImpactRuntimeContextArmTemplate renders one arm of the
// runtime-context UNION (#11447 Plan C): the byte-identical
// active-generation joins, repository-decoder lateral, grant predicate, and
// final decoded-membership predicate, parameterized only by the per-arm kind
// predicate and the arm prefilter (the three %s verbs in order). Arms are
// superset prefilters only — the decoder and the final predicate do the
// exact filtering, never arm-specific semantics. Parameter ordinals are
// stable across arms so one bind list serves the whole UNION: $1 candidates,
// $2/$3 scoped grants, $4 the candidate scope closure (each candidate plus
// its git-repository-scope envelope form, derived by
// supplyChainRuntimeContextClosureScopes).
const selectSupplyChainImpactRuntimeContextArmTemplate = `
SELECT fact.fact_kind,
       fact.scope_id,
       fact.payload,
       runtime_repository.repository_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
%s
WHERE %s
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (%s)
  AND (
        (
          COALESCE(cardinality($2::text[]), 0) = 0
          AND COALESCE(cardinality($3::text[]), 0) = 0
        )
        OR runtime_repository.repository_id = ANY($2::text[])
        OR fact.scope_id = ANY($3::text[])
      )
  AND runtime_repository.repository_id = ANY($1::text[])`

// supplyChainRuntimeContextKindList renders the closed kind set as inline
// literals from the single source RuntimeContextFactKinds. Literals (not a
// kinds parameter) let the planner prove kind-partial index predicates at
// plan time.
func supplyChainRuntimeContextKindList() string {
	quoted := make([]string, 0, len(RuntimeContextFactKinds))
	for _, kind := range RuntimeContextFactKinds {
		quoted = append(quoted, "'"+kind+"'")
	}
	return "fact.fact_kind IN (" + strings.Join(quoted, ", ") + ")"
}

// supplyChainRuntimeContextClosureScopes derives the scope closure for a
// candidate set: each candidate plus its git-repository-scope envelope form.
// The closure arm probes this set; the per-kind outside arms exclude it, so
// the arms are disjoint by construction and UNION ALL needs no dedup.
func supplyChainRuntimeContextClosureScopes(repositoryIDs []string) []string {
	seen := make(map[string]struct{}, 2*len(repositoryIDs))
	out := make([]string, 0, 2*len(repositoryIDs))
	for _, id := range repositoryIDs {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		for _, scopeID := range []string{id, scope.GitRepositoryScopePrefix + id} {
			if _, dup := seen[scopeID]; dup {
				continue
			}
			seen[scopeID] = struct{}{}
			out = append(out, scopeID)
		}
	}
	return out
}

// buildSelectSupplyChainImpactRuntimeContextQuery assembles the UNION ALL
// production shape (#11447 Plan C): one scope-closure arm over all four
// kinds plus one bounded outside-closure arm per kind. Kept beside the
// shared decoder it renders so the byte-identical fragments stay in one
// file; the store owns only the assembled constant and its contract docs.
func buildSelectSupplyChainImpactRuntimeContextQuery() string {
	decoder := supplyChainRuntimeRepositoryDecoderJoin(
		"fact.payload",
		"fact.scope_id",
		"runtime_repository",
	)
	arms := make([]string, 0, 1+len(RuntimeContextFactKinds))
	arms = append(arms, fmt.Sprintf(
		selectSupplyChainImpactRuntimeContextArmTemplate,
		decoder,
		supplyChainRuntimeContextKindList(),
		"scope.scope_id = ANY($4::text[])",
	))
	for _, kind := range RuntimeContextFactKinds {
		arms = append(arms, fmt.Sprintf(
			selectSupplyChainImpactRuntimeContextArmTemplate,
			decoder,
			"fact.fact_kind = '"+kind+"'",
			"NOT (fact.scope_id = ANY($4::text[]))",
		))
	}
	return strings.Join(arms, "\nUNION ALL\n")
}
