// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "fmt"

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
