# #8812: per-definition content-store policy (metadata and redact)

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md);
the issue bodies stay the binding acceptance lists. This note holds the
measurements. Every figure below was produced on this branch.

## What changed

- `parser.Definition.ContentStore` names `store`, `metadata`, or `redact`
  (marker mode, with a credential-key list). `NewRegistry` validates it.
  `Registry.Fingerprint` includes a non-store policy.
- `gitcontent.ApplyContentStore` applies it at the collector write boundary, in
  two places because the body exists at two stages: before `shape.Materialize`
  (so `source_cache` is cut from the stored body) and in `streamFacts` (which
  re-reads the body from disk). The digest stays on the source bytes.
- `go/internal/secret/detect` holds the migration 138 pattern and CASE arms as
  Go values. `internal/query` aliases `hardcodedSecretSQLPattern` to it, and
  the existing migration binding test now pins both.
- No production definition binds a non-store policy (the arbiter's ordering:
  #8833's marker-arm migration lands first).

## Performance Evidence

Performance Evidence: the marker redactor and the policy lookup, measured below.
Host: 16 vCPU arm64, Go 1.26.6, PostgreSQL 18.6 for the live proof.
Same benchmark, same inputs, before and after the prefilter commit
(`BenchmarkRedactMarker`, `go test -bench`, 3 runs, the spread was under 1%):

| Body | Before (MB/s) | After (MB/s) | After (ns/op) |
| --- | --- | --- | --- |
| typical `.yarnrc.yml`, 4 lines | 3.75 | 14.55 | 8,935 |
| 1 MiB, no credential | 3.23 | 102.4 | 10,034,619 |
| 1 MiB, a credential on every fifth line | 12.06 | 8.57 | 104,069,715 |

The prefilter is two lowercase substring checks per line (the detector words,
the listed keys) that keep the regular expressions off a line that cannot match.
The dense body got slower (12.06 to 8.57 MB/s) because every line there matches,
so it pays the prefilter and then the regular expressions, and the YAML
fail-closed state added in the same change. It is the worst case: a 1 MiB
governed file costs about 0.12 s per pass, twice per collection (snapshot and
emit). Governed files are small configuration files.
`TestSpansPrefilterNeverHidesAMatch` pins that the prefilter never hides a match.

No-Regression Evidence: the cost every repository pays today, with no
non-store policy bound, is one registry flag check per snapshot and one nil-map
read per emitted file (`BenchmarkContentStoreUngoverned`, 3 runs):

| Stage | ns/op | allocs |
| --- | --- | --- |
| snapshot stage, 5,000 files | 8.86 | 0 |
| emit stage, per file | 1.82 | 0 |

`Registry.ContentStoreForPath` returns before any lookup while no definition is
governed, so `LookupByPath` (which clones the definition) is never called for a
repository file. The default registry's `Fingerprint` is unchanged because a
`store` policy renders as nothing, so no freshness hint moves and no repository
re-collects from this change alone. The first fleet-wide re-collect happens when
#8833 binds the first definition.

## Observability Evidence

Observability Evidence: one new counter and two structured log lines.
New counter `eshu_dp_collector_content_store_files_total{policy,outcome}`
(`policy` is `metadata` or `redact`; `outcome` is `metadata`, `redacted`,
`clean`, or `redactor_failed`), one add per (policy, outcome) per fact stream.
The stream logs `git content store policy applied` (INFO) with `values_replaced`
and one count per policy and outcome, or `git content store policy fell back to
metadata rows` (WARN) when any file failed. At 3 AM: a non-zero
`redactor_failed` rate means a credential-bearing file is silently losing its
body; the WARN log names the repository, scope, and generation.
`TestLegacyContentFilesPathAppliesThePolicyAndCountsOutcomes` reads the counter
from a real `telemetry.Instruments` and asserts the log carries no credential.
Documented in `docs/public/reference/telemetry/metrics.md` and
`docs/public/observability/telemetry-coverage.md`;
`scripts/verify-telemetry-coverage.sh` passes.

## Seeded-violation proofs (RED, then GREEN)

Each test was run against the old behavior or a deliberate mutation first.

| Test | Mutation | Result |
| --- | --- | --- |
| `TestContentStorePolicyHealsStoredRowsOnTheNextGeneration` (live, 4 subtests) | the next generation stores the verbatim body (no policy) | FAIL: seed in `content`, `content_lower`, `source_cache`, `content_file_secret_lines.line_text`; metadata row kept 1 secret line and 1 reference |
| `TestFreshnessHintIsStableAcrossCyclesAndPolicies`, `TestMetadataPolicyEmits...`, `TestRedactPolicyEmits...` | recompute the digest from the stored body | FAIL: `content_digest` and the freshness hint moved |
| `TestRedactMarkerFailsClosedOnYAMLValueShapes` | never open the continuation state | FAIL on 9 of 13 shapes (block, folded, tagged, anchored, empty, same-indent sequence, multi-line plain) |
| `TestApplyContentStoreFailsClosed` | n/a (written first; the function did not exist) | build failure, then GREEN |

The live proof seeds first: it asserts the verbatim row set (content,
`content_lower`, a derived secret line, a derived reference, an entity
`source_cache`) before projecting the next generation, so it cannot pass on a
fresh database. It runs the production projector runtime, content writer, and
the migration 138 triggers, on a full snapshot and on a delta generation, for
`metadata` and `redact`. Run: PostgreSQL 18.6, 4 subtests, 11.2 s.

## Per-kind result under migration 138

`TestRedactMarkerPerKindResultUnderMigration138` pins, for each finding kind, a
key line and a free-text line. Probe values are fabricated.

| Kind | Key line, raw to marked | Free-text line, raw to marked | Under 138 |
| --- | --- | --- | --- |
| `aws_access_key` | `access_key_id = "AKIA<16>"` to `access_key_id = "REDACTED_aws_access_key"` | `# rotate AKIA<16> soon` to `# rotate REDACTED_aws_access_key soon` | lost |
| `slack_token` | `slack_webhook = "xoxb-<10+>"` to `slack_webhook = "REDACTED_slack_token"` | `# post with xoxb-<10+> here` to `# post with REDACTED_slack_token here` | lost |
| `private_key` | `key = "-----BEGIN RSA PRIVATE KEY-----"` to `key = "REDACTED_private_key"` | `# -----BEGIN RSA PRIVATE KEY-----` to `# REDACTED_private_key` | lost |
| `api_token` | `npmAuthToken: <12>` to `npmAuthToken: REDACTED_api_token` | `# the api_key=<12> was pasted` to `# the api_key=REDACTED_api_token was pasted` | survives |
| `password_literal` | `db_password = "<12>"` to `db_password = "REDACTED_password_literal"` | `# password: <12> per ops` to `# password: REDACTED_password_literal per ops` | survives |
| `secret_literal` | `client_secret = "<12>"` to `client_secret = "REDACTED_secret_literal"` | `# the client_secret = <12> shared` to `# the client_secret = REDACTED_secret_literal shared` | survives |

Kind changes (also pinned): `npmAuthToken: REDACTED_aws_access_key` (an `AKIA`
value misplaced under a listed key) and `slack_token = "REDACTED_slack_token"`
are detected as `api_token`. `db_password = "[redacted]"` and `db_password =
"***"` are not detected, which is why those markers are rejected. A 1 to 5
character value under `npmAuthToken` is redacted. These results match the
evidence page's marker probe; #8833's marker arm changes the three "lost" rows
and the two kind changes to "same kind".

## Review findings addressed

An automated security review of the redactor found three classes of fail-open
shape, all fixed with tests: a block scalar with a trailing comment, tag, or
anchor; a quoted scalar that closes on a later line; and a keep-verbatim path
(`${NAME}`, an earlier marker) that ignored what followed the value. The fix:
a listed key always opens a continuation, an unterminated quote redacts to its
closing quote, YAML comment/tag/anchor syntax is read only after a colon, and a
value survives only when the whole remainder is exactly an environment
reference or a marker.

## Findings for #8833

- The yaml and hcl parsers echo structured values (the whole file as
  `parsed_file_data.source`, and for HCL a variable `default` in
  `entity_metadata` and `parsed_file_data`). The policy replaces the whole-file
  `source` copy and entity source snippets with the stored body, but a parser's
  extracted values are parser output and stay. A definition bound to `redact`
  needs its parser output checked for credential values; the tfvars differential
  is the place.
- The yaml parser skips a file with invalid UTF-8 before any fact exists, so the
  fail-closed path for a `.yarnrc.yml` is reached only through `raw_text`-style
  parsers; the collector-level test uses `raw_text`.
- `.yarnrc.yml` resolves to the `yaml` definition by extension. A `redact`
  binding for one file name needs a dedicated exact-name definition, because the
  policy is per definition.

## NOT_CHECKED

- The registry-URL fact for a seeded `.npmrc` (issue AC 1): no `.npmrc` parser
  exists, so the collector test asserts the file fact survives and the body is
  empty; a registry-URL fact needs a parser that #8812 does not add.
- A deployment census and a real-corpus count of marker-arm false positives
  (#8833).
- The engine-parity test of Go RE2 against Postgres ARE over the gap-table
  corpus (#8833). `Spans` uses Go `(?i)` folding, which is wider than Postgres
  ARE for a few non-ASCII runes, so redaction can only find more than the
  migration, not fewer.
- `make pre-push`, `make pre-pr`, and the CI-only gates (the coordinator runs
  them).
