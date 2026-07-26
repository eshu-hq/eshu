// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "fmt"

// supplyChainRuntimeRepositoryDecoderJoin renders the canonical SQL decoder
// for a runtime fact's repository anchor. Its precedence must match
// supplyChainRuntimeContextRepositoryID: direct payload repository, payload
// scope, envelope scope, then the first repository-like related scope.
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
           NULLIF(BTRIM(%[1]s->>'repository_id'), ''),
           NULLIF(BTRIM(%[1]s->>'repo_id'), ''),
           CASE
             WHEN BTRIM(%[1]s->>'scope_id') LIKE 'repository:%%'
               THEN BTRIM(%[1]s->>'scope_id')
             WHEN BTRIM(%[1]s->>'scope_id') LIKE 'git-repository-scope:%%'
               THEN NULLIF(BTRIM(SUBSTRING(%[1]s->>'scope_id' FROM 22)), '')
           END,
           CASE
             WHEN BTRIM(%[2]s) LIKE 'repository:%%'
               THEN BTRIM(%[2]s)
             WHEN BTRIM(%[2]s) LIKE 'git-repository-scope:%%'
               THEN NULLIF(BTRIM(SUBSTRING(%[2]s FROM 22)), '')
           END,
           (
             SELECT CASE
                      WHEN BTRIM(related.value) LIKE 'repository:%%'
                        THEN BTRIM(related.value)
                      WHEN BTRIM(related.value) LIKE 'git-repository-scope:%%'
                        THEN NULLIF(BTRIM(SUBSTRING(related.value FROM 22)), '')
                    END
             FROM jsonb_array_elements_text(
               CASE
                 WHEN jsonb_typeof(%[1]s->'related_scope_ids') = 'array'
                   THEN %[1]s->'related_scope_ids'
                 ELSE '[]'::jsonb
               END
             ) WITH ORDINALITY AS related(value, ordinal)
             WHERE BTRIM(related.value) LIKE 'repository:%%'
                OR BTRIM(related.value) LIKE 'git-repository-scope:%%'
             ORDER BY related.ordinal
             LIMIT 1
           )
         ) AS repository_id
) AS %[3]s ON TRUE`,
		payloadExpr,
		scopeExpr,
		alias,
	)
}
