# Acceptance criteria for the credential-file record (#8813)

These are the acceptance lists for the follow-ups to
[the #8813 record](8813-credential-file-exposure.md). They moved out of that
record to keep both files under the 500-line cap.

The issue bodies are the binding acceptance lists. If this page and an issue
body ever differ, the issue body wins. The lists below follow them.

**#8812 owns** the enum, marker redactor, digest rule, fingerprint,
`source_cache` rule, upgrade test, and policy docs. It does not own the detector
migration, the engine-parity test, the gap-table corpus, or the `user:pass@`
case, because migration 138 does not detect it.

- [ ] A `metadata` definition never writes a body, by test. A seeded `.npmrc`
  with a literal `_authToken` produces the registry-URL fact and a row with empty
  `content` and `content_lower`, `line_count = 0`, `content_hash` the source
  digest, and no `content_file_secret_lines` or `content_file_references` row.
- [ ] Upgrade test: a seeded verbatim `content_files`, reference, and
  `content_entities` row (non-empty `source_cache`) for a path that becomes
  `metadata` is empty after the next projected generation, on a full-snapshot
  and a delta generation. No tombstone, no `RetainedPaths` change.
- [ ] `Registry.Fingerprint` includes each definition's policy, with a test; the
  PR body states the one-time re-collect.
- [ ] `source_cache` is empty under `metadata` and sliced from the redacted body
  under `redact`; a seeded credential never reaches `content`, `content_lower`,
  `line_text`, or `source_cache`.
- [ ] The `content` fact carries the empty or redacted body and the source
  digest; an unchanged file keeps the same freshness hint across two cycles.
- [ ] A per-kind table test over all six finding kinds, a key line and a
  free-text line for each. Under migration 138 it pins the measured result
  (survives, lost, or kind changed). #8833 updates it to "same kind".
  `[redacted]` and `***` are rejected by test. The trigger is the definition's
  credential-key list, and a 1 to 5 character value under `npmAuthToken` is
  redacted.
- [ ] A redactor error or non-UTF-8 input stores a `metadata` row and increments
  `redactor_failed`; no raw-body fallback exists, by test.
- [ ] The marker redactor binds to `hardcodedSecretSQLPattern`, pinned by test.
  It redacts the values of the definition's credential-key list at any length,
  plus any detector match; no production definition binds to marker mode before
  #8833's marker-arm migration lands.
- [ ] `redact` carries a per-definition redactor mode. #8812 ships `marker`
  (closed-schema files only, with a per-definition credential-key list), so
  #8833 can add `skeleton` without a second policy value.
- [ ] Docs carry the policy table and say `content_hash` is the digest of the
  source file, not of the stored body.
- [ ] Independent deep review of the write path and the redaction patterns.

**#8833 owns** the tfvars and `.yarnrc.yml` bindings, the detector migration, the
engine-parity test, the gap-table corpus, the differential, the operator SQL,
the retention check, and the census.

- [ ] Seeded `.yarnrc.yml` (literal `npmAuthToken`, `user:pass@` registry URL)
  stores neither value, keeps the host, and the token line is still reported. A
  misplaced `AKIA` value under `npmAuthToken` keeps `aws_access_key`, a comment
  line holding an `xoxb-` token keeps `slack_token`, a 5-character
  `npmAuthToken` is redacted, and `${NPM_TOKEN}` is kept.
- [ ] Seeded tfvars and `.tfvars.json` (password, secret-named block, heredoc,
  token in a comment, `user:pass@` URL) store none of the values; the password
  line is reported at its original line; parser facts and config-artifact rows
  survive; `source_cache` holds only skeleton text.
- [ ] One migration adds the detector extras and the marker arm (first in the
  `CASE`, in the line gate, and in the Go constant) and re-derives
  `content_file_secret_lines`; finding-count delta and lock window are recorded
  first, with marker-arm hits on files outside a redact policy reported
  separately; an engine-parity test compares Go RE2 and Postgres ARE over the
  gap-table corpus.
- [ ] After one collection cycle on a database holding verbatim rows,
  `eshu_dp_content_policy_raw_rows` reads zero (the detector-bounded test: an
  unsuppressed `content_file_secret_lines` row on a policy path whose
  `line_text` carries no `REDACTED_<finding_kind>` marker; the test seeds the
  old rows first).
- [ ] The operator SQL sets `content = ''`, deletes the reference rows, blanks
  `source_cache`, clears the active generation's search documents and terms, and
  deletes the matching active `reducer_eshu_search_document` fact rows by
  `fact_id`; 500 rows per statement; idempotent; run order documented. After it
  runs, semantic search `context_text` for a matched path holds no seeded
  credential, by test, and the cleanup runbook's census and exit criterion read
  zero.
- [ ] A retention check runs the pruning path against a `content` fact, a
  content-entity fact, a `reducer_eshu_search_document` fact, and the
  `eshu_search_index_documents` and `eshu_search_index_terms` rows; if any is
  exempt, the payload decision reopens. The docs carry the operator note (7 to
  90 days, dead tuples, WAL, backups).
- [ ] A deployment census is recorded as counts, before and after.
- [ ] Property test over skeleton bodies, with a credential-shape corpus that
  never survives (a PEM `BEGIN`..`END` block in a comment included) and a
  fail-closed unterminated-string line.
- [ ] Differential before the fingerprint flip: `DiscoverEvidence` agrees
  between skeleton and raw body, and the `(line_number, finding_kind)` check of
  `eshu_secret_line_findings` runs per kind over all six kinds, for marker and
  skeleton lines. Every `REDACTED_value` loss is listed; each difference is
  closed or accepted on #8833.
- [ ] Independent deep review of the write path, redactor, and migration.
