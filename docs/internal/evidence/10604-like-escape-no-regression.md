# Fix 05b LIKE-Escape No-Regression Evidence (#10604)

Content-search patterns (`search_file_content`, `search_entity_content`,
scoped and unscoped) now bind the pre-escaped LIKE literal
(`querycontract.EscapeLikeLiteral`: `\` → `\\`, `%` → `\%`, `_` → `\_`)
instead of the caller's raw string. Classification (trigram presence),
telemetry pattern class, rerank, and the rank walk keep reading the raw
pattern; only the SQL bind sees the escaped form.

No-Regression Evidence (#10604): same shipped statements, same indexes, one
string transform at the bind sites. The differential
`TestLikeEscapeDifferentialLive` (fixed corpus with plain, `%`, `_`, `\`,
mixed code-like, and non-ASCII rows over two repositories; scoped 1+N plus
unscoped file and entity paths; Go `strings.Contains` oracle on lowercased
content) FAILS on the base binds — `han_ler` also matches `handler` rows,
`%` and `_` match the whole corpus, `\` matches nothing — and passes with
the escape on every path and page window. Existing suites green on the same
tree: scoped custom/generic equivalence (incl. the S7 literal and the
percent edge), unscoped file/entity old-statement equivalence, UNION ALL
probe equivalence, large-document ILIKE case-folding differential,
trigram/statement/derivation guards, fallback builder tests, and the
`./internal/query/...` + `./internal/mcp` + affected
`./internal/storage/postgres` suites. Backend: local PostgreSQL 18,
pg_trgm 1.6, disposable databases. Plan check: on a 20,001-row corpus the
escaped bind for `get_%_value` (`get\_\%\_value`) still plans a Bitmap
Index Scan on `content_files_content_trgm_idx`; escaping can only change
plans at the margin because it never creates an alphanumeric run, only
breaks on the inserted backslash, and classification of the raw form always
equals classification of the bound form (pinned by
`TestWalkBindsEscapedLiteralWhileClassifyingRaw`).

No-Observability-Change: no new spans, metrics, logs, or statuses; the
existing `search.*` telemetry is untouched. Cross-reference, not duplicate:
fix 04's `wildcard` pattern class will visibly empty after this lands, which
is the intended effect of literal matching.
