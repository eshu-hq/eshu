# #8833 part 2a: detector migrations 234 and 235 and the credential-file bindings

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md),
and the issue body of #8833 is the binding acceptance list. This note holds the
measurements for part 2:

- the dedicated `tfvars` and `yarnrc` definitions and their bindings;
- migrations 234 and 235 (ruling-3's split form): 234 adds `policy_version`
  to the state row, and 235 carries the marker arm and the detector extras and
  moves the state to `not_built`;
- the evidence shapes;
- the `eshu_dp_content_policy_raw_rows` gauge.

Every figure below was produced on this branch, against PostgreSQL 18.6 in a
scratch container on a shared 16 vCPU arm64 host with Go 1.26.6. Load average
ran 10 to 44 during the runs, so timings are bands, not points.

## Corpora

| Name | Content | Used for |
| --- | --- | --- |
| Scale corpus | 150,000 git-tracked text files (1.66 GB) from the eshu checkout and the Go module cache, loaded into a minimal `content_files` | finding-count delta, derivation cost |
| tfvars sample | 450 public `*.tfvars` files from GitHub code search (eight queries; two target `app_repo` and `github.com`, so the sample leans toward evidence-bearing files) | evidence and findings differentials |
| `.yarnrc.yml` sample | 220 public files from GitHub code search | marker-mode findings differential |

Only counts are recorded. No file content leaves the scratch host.

## Performance Evidence

Performance Evidence: finding-count delta and derivation cost of the new function
against migration 138, on the scale corpus.

Finding counts, 138 against the new function (measured before the 234/235 split):

| Kind | Migration 138 | New function |
| --- | --- | --- |
| api_token | 12,485 | 19,133 |
| secret_literal | 2,085 | 2,529 |
| password_literal | 1,472 | 2,315 |
| private_key | 275 | 276 |
| aws_access_key | 82 | 88 |
| slack_token | 4 | 9 |
| total | 16,403 | 24,350 |

Re-measured after the split, on the same corpus, with the shipped 235 function
(case-sensitive marker arms): the repair published 24,359 rows (api_token
19,133, secret_literal 2,529, password_literal 2,324, private_key 276,
aws_access_key 88, slack_token 9). The table above is from an earlier revision
of the function; the 9 extra password_literal lines are not attributed here.
The corpus holds no lower-case `redacted_<kind>` spelling, so the case-sensitive
marker arm changes no count on it.

None lost. 7,947 gained (7,956 with the shipped function). Four changed kind, all from api_token: two to slack_token and two to aws_access_key, because the marker arm comes first on lines that hold a literal marker.

Gained lines by the extra that explains them:

| Extra | Kind | Lines | Files |
| --- | --- | --- | --- |
| JSON-quoted keys | api_token | 6,646 | 680 |
| JSON-quoted keys | secret_literal | 440 | 140 |
| JSON-quoted keys | password_literal | 160 | 90 |
| `user:pass@` userinfo | password_literal | 683 | 358 |
| marker arm, on files outside any policy | aws_access_key, slack_token, private_key | 8 | 2 |
| HCL block openers | secret_literal | 3 | 2 |
| npmAuthIdent | any | 0 | 0 |

Notes on the gained lines:

- The JSON-quoted gains (`"input_token": "..."`) are in the same precision
  class as migration 138's existing YAML rule.
- 443 of the 683 userinfo lines carry a suppression word.
- The 8 marker-arm gains, plus the 4 kind changes, are 12 lines in 2 eshu files,
  none on a test path. These are the marker-arm hits outside a redact policy
  that #8833 asks to be reported separately.

The block-opener extra was narrowed after measurement. The first draft,
`(key)s?['"]?\s*[:=]\s*{`, added 25,301 lines in 2,309 files, mostly JSON API
schema keys such as `"nextPageToken": {`. The final form matches only the HCL
`=` form, needs the key word to end the key, and needs the brace to end the
line.

Derivation cost, measured on 10,000 files from the scale corpus, two
interleaved rounds:

| Detector | Seconds per 10k files |
| --- | --- |
| migration 138 | 7.4 to 7.6 |
| new function with the marker as a case-insensitive ARE alternative | 12.3 to 13.2 |
| new function with the marker as `strpos(.., 'REDACTED_')` OR the regex (shipped) | 9.2 to 10.1 |
| new function with no marker gate at all | 9.0 to 9.1 |
| 138 gate with all new CASE arms | 7.3 to 7.5 |

Moving the marker alternative first, or spelling it `REDACTED_[a-z_]*(key|token|literal)`, did not help (12.0 to 12.7 s). The extra CASE arms cost nothing measurable.

The shipped detector costs 24 to 33% more per derived file than migration 138.
It runs in the content_files trigger on every changed write and in the
bootstrap finalizer. At the measured rate this is about 0.2 ms more per file.
The QA environment figure is not measured here: migration 138 recorded
0.27 ms per file in the QA environment, so the shipped detector would scale to
about 0.35 ms there.

Time-to-ready (ruling-3 Decision 6), on the scale corpus with the shipped
234 and 235 and part 2b's `lines.Repairer` (default `Finalize` settings: 4
workers, 500-file batches). The host load average ran 3 to 25.

| Step | Result |
| --- | --- |
| apply 234 / apply 235 | 17.0 ms / 11.4 ms; neither locks `content_files` |
| re-apply 235 after a reset to policy 138 | 13 to 43 ms |
| repair, no writers | 42.7 s and 43.5 s; 856 batches, 0 retries; parity 0/0 |
| repair under 3 writers (50-file UPDATEs on random repositories) | 51.8 s, 0 retries; parity 0/0 |
| writer statements during that repair | 2,877; p50 19 ms, p99 361 ms, max 10.67 s |
| the same writers, no repair, 50 s | 3,976; p50 10 ms, p99 282 ms, max 1.11 s |
| legacy-scan read during the window, limit 50 / 200 | 0.16 to 0.21 s / 0.67 s (the plan walks `content_files_pkey` and stops at the limit) |
| legacy-scan read with a rare-kind filter (`private_key`, `slack_token`), limit 200 | 123 to 161 s per call (no early stop; this corpus has no trigram index) |

The repair stays under ruling-3's 160.9 s threshold. The QA environment
recorded 0.27 ms per file for migration 138; at the 24 to 33% derivation cost
above, a 145k-file repair there projects to the same tens of seconds. The
projection is not measured.

Finding: the writer max of 10.67 s is one finalizer batch. Finalize bounds a
batch by file count only, and 25 batches took over 500 ms; the worst were 500
files holding 72 MB in 11.3 s. A writer that touches any row of such a batch
waits for its whole derivation. A shim on that repository's first window
derived 2 MiB in 0.15 s, 4 MiB in 0.47 s, 8 MiB in 1.04 s, and the full 72 MB
in 8.9 s. The coordinator assigned a byte bound on each batch to part 2b (the
reducer repair); it is not in this change.

No-Regression Evidence: the per-file policy lookup that every repository now
pays (the default registry binds two definitions), measured with
`BenchmarkContentStoreGovernedRegistry` (now the `metas_5000_files` and
snapshot sub-benchmarks of `BenchmarkContentStoreGovernedDefault`) over 5,000
ungoverned files:

| Stage | Before | After |
| --- | --- | --- |
| snapshot stage | 1.11 ms, 5,001 allocations | 0.92 ms, 1 allocation |
| meta stage | 1.12 ms, 5,000 allocations | 0.90 ms, 0 allocations |

The improvement comes from `ContentStoreForPath` resolving through a
non-cloning `lookupPath`.

Trigger-path cost (ruling-2's A/B, stop threshold 1.35x of 138):
`TestContentFileSecretLinesWriteCostLive`, the 7125 harness, on 12,000 real
files through the production `ContentWriter`. Each arm has five rounds with
the triggers on and off, and the arms were run interleaved twice, base 138
(`7ce9439814`) then 235. Postgres backend CPU per file (triggers on minus
off; median of 5) is the comparison, because wall time swung with the shared
host's load (4 to 40):

| Write path | 138, run 1 / 2 | 235, run 1 / 2 | Ratio |
| --- | --- | --- | --- |
| insert | 0.340 / 0.328 ms | 0.446 / 0.437 ms | 1.31x / 1.33x |
| changed upsert | 1.269 / 0.720 ms | 0.878 / 0.809 ms | 0.69x / 1.12x |
| unchanged | 0.000 / 0.000 ms | -0.003 / 0.003 ms | none |

The insert path stays under the 1.35x threshold. The harness reads its corpus
from each checkout, and part 2a's tree holds more credential-shaped fixtures:
235 derived 2,820 side rows against 138's 2,281. That inflates 235's figure;
a same-corpus run is NOT_CHECKED. The deferred-session (bulk-load) path costs
within 1% of no triggers in both arms.

No-Regression Evidence: the YAML structure check that marker mode runs after
redacting a `.yarnrc.yml` (#8833 security review). It parses only a body that
holds `*`, `?`, a backslash, `{`, or `[`; the last two came with the flow-style
fix and rule that a listed key's value must not reach a line the redactor left
alone (`marker_yaml.go`). A body without one of the five bytes costs five
vectorized `IndexByte` passes. `BenchmarkRedactMarker`,
interleaved against part 2a's head before the flow rule (`ecb7e9cd70`), two
rounds of three each, medians:

| Body | Part 1 | Part 2a before the flow rule | With the flow rule |
| --- | --- | --- | --- |
| typical `.yarnrc.yml` | 7.1 µs | 25.2 µs | 24.9 µs |
| clean 1 MiB | 11.0 ms | 35.0 ms | 35.0 ms |
| dense 1 MiB | 106.1 to 106.2 ms | 156.8 ms | 155.6 ms |
| URL keys | 49.5 to 49.8 µs | 87.5 µs | 87.2 µs |
| typical with a flow scope (parses) | n/a | 66.6 µs | 79.7 µs |
| 1 MiB list of flow entries (parses) | n/a | 25.3 ms | 86.3 ms |

The first four bodies hold none of the five bytes, so they do not move. A body
that parses pays the parse: 13 µs on a typical file with
a flow scope, and 61 ms per MiB on a file that is all flow entries, which a
`.yarnrc.yml` never is. Without the byte prefilter the clean 1 MiB body took
74 ms. A repository holds one or two small `.yarnrc.yml` files, so the added
cost is tens of microseconds per repository.

Fail-closed rule, exactly (`marker_yaml.go`): for each listed key that is not
inside another listed key's value, the value is taken to reach every source line
from the line after the key to the line before the next node starts, plus the
text before that node on its own line. The text that could be value on those
lines must be nothing (blank, a comment, only flow brackets and commas, a `---`
or `...` marker), or lie inside the single span the redactor replaced with one
`REDACTED_<kind>` marker on that line (the text before and after the marker must
equal the source), or be exactly a marker or `${NAME}` reference an earlier
pass kept. Marker text beside other text fails; so does a line whose replaced
span stops short of the tail. For a body yaml.v3 cannot parse (it rejects a tab
as flow indentation and a bare document after `...`; js-yaml reads both), the
lines after the separator of a line a listed key matches are followed with a
lexical bracket and quote count: while a bracket or quote the value opened is
open every line is value and is held to the same text rule (a leading closer
or comma does not end it); once both are closed, a bracket-only line or an
entry line ends it. A line inside a quoted scalar (found by reading the source
for the closing quote in the parsed path, by the quote count in the lexical
one) gets no comment or marker shortcut: only white space is nothing. The reason is the existing
`yaml_indirection`; no new label value.

Completeness claim, and where it stops: for a body yaml.v3 parses, no byte of a
listed key's decoded value after the key's own line is stored in the clear (the
line rule covers the key's line from the separator to the end of the line). For
a body yaml.v3 rejects, the lexical rule is a heuristic that was swept against
js-yaml and PyYAML, not proven; a shape found there becomes a hostile seed. A
value under an unlisted key is the existing denylist limit.

Accuracy proof for the rule: `TestRedactMarkerNeverStoresADecodedCredential`
generates 150,000 bodies per run from a fragment pool (flow and block openers,
listed keys with and without inline values, tabs, comments, document markers,
marker text, URL-key and detector lines, 70-deep nesting, closers), parses each
with yaml.v3 as an oracle independent of the redactor, and requires that no
token decoded under a listed key survives. A one-off run of 4.5 million bodies
over three seeds found none. `TestRedactMarkerNoParserDecodedCredentialSurvives`
sends 6,000 bodies per run (500,000 in two one-off runs) through js-yaml 4.1.1
(Yarn's parser) and PyYAML as well and requires that a token any of the three
decodes under a listed key is gone from the stored body; it skips a parser that
is not installed. The js-yaml run found 9 leaks in a first 350,000-candidate
sweep, all from `...` or tab shapes yaml.v3 rejects, which the lexical fallback
now covers. With the text rule switched off, 32 of the 40 hostile seeds store
the token and both oracle tests fail; the other 8 are held by the redactor's
own fixes (a comment line no longer ends a continuation, and `-<tab>` is a
sequence item) or by its quoted scalar and detector handling.

## Differentials

Evidence differential (`TestTfvarsSkeletonEvidenceDifferential`, tfvars sample).
The catalog holds every name-like token in the raw bodies, and evidence is
compared by kind, target, path, and line:

| Skeleton | Raw evidence | Skeleton evidence | Lost | Gained |
| --- | --- | --- | --- | --- |
| plain allowlist | 445 | 0 | 445 | 0 |
| with the record's evidence shapes (shipped) | 445 | 433 | 12 | 0 |

The 12 lost items, listed for acceptance on #8833:

- 7 module-source matches from one file, where a `git@github.com:o/r//sub/?ref=x` source is normalized and its `ref` dropped;
- 3 `/api/<name>/` config-path matches inside non-github URLs;
- 1 github URL whose owner holds a `;`;
- 1 `codeload.github.com` subdomain. The host must follow a boundary, so a github.com string inside another URL's query cannot pass.

Findings differential: `eshu_secret_line_findings` (the new function) over the
raw and the stored bodies of both samples, comparing `(line_number,
finding_kind)` per line.

| Sample | api_token | password_literal | private_key | secret_literal | Lost | Gained |
| --- | --- | --- | --- | --- | --- | --- |
| tfvars, skeleton | 20 = 20 | 108 = 108 | 1 = 1 | 31 = 31 | 0 | 0 |
| `.yarnrc.yml`, marker | 37 raw, 83 stored | 0 | 0 | 0 raw, 2 stored | 0 | 48 |

The 48 `.yarnrc.yml` gains are values under a listed key that the ruling says
are redacted, and their marker is a finding: `${NAME:-literal}` and
`${NAME-literal}` defaults (36), CI template syntaxes such as `${{ }}`,
`#{}#`, and `${env:N}` (9), a typo URL (2), and a commented token (1).

The six-kind check for marker and skeleton lines also runs in Go:
`TestRedactSkeletonPropertyCorpus` asserts exact per-line parity on 3,000
seeded bodies, and `TestSecretDetectorEngineParity` proves the Go and Postgres
engines agree.

## Observability Evidence

Observability Evidence: `eshu_dp_content_policy_raw_rows` counts the
unsuppressed side-table lines on governed paths that carry no
`REDACTED_<finding_kind>` marker and are not a value-free block opener. The
reducer serves it from the Postgres gauge snapshot refresher, under the snapshot
label `reducer_content_policy_raw_rows`, so a scrape never queries.

`TestContentPolicyRawRowsHealsAfterTheNextGeneration` seeds verbatim rows
(8 raw lines), then stores the next generation through the policies, and the
gauge reads 0. On the scale corpus the count is a 32 ms sequential scan of the
side table.

The policy counter from part 1 carries the `redactor` label. A non-zero
`{redactor="skeleton",outcome="redactor_failed"}` means a tfvars body fell
back to a metadata row.
