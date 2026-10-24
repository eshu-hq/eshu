# #8833 part 2: detector migration 234 and the credential-file bindings

Part of #8807. The design record is
[8813-credential-file-exposure.md](../design/8813-credential-file-exposure.md),
and the issue body of #8833 is the binding acceptance list. This note holds the
measurements for part 2:

- the dedicated `tfvars` and `yarnrc` definitions and their bindings;
- migration 234, which carries the marker arm and the detector extras;
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

Performance Evidence: finding-count delta and derivation cost of migration 234
against migration 138, on the scale corpus.

Finding counts, 138 against 233:

| Kind | Migration 138 | Migration 234 |
| --- | --- | --- |
| api_token | 12,485 | 19,133 |
| secret_literal | 2,085 | 2,529 |
| password_literal | 1,472 | 2,315 |
| private_key | 275 | 276 |
| aws_access_key | 82 | 88 |
| slack_token | 4 | 9 |
| total | 16,403 | 24,350 |

None lost. 7,947 gained. Four changed kind, all from api_token: two to slack_token and two to aws_access_key, because the marker arm comes first on lines that hold a literal marker.

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
| 233 with the marker as a case-insensitive ARE alternative | 12.3 to 13.2 |
| 233 with the marker as `strpos(.., 'REDACTED_')` OR the regex (shipped) | 9.2 to 10.1 |
| 233 with no marker gate at all | 9.0 to 9.1 |
| 138 gate with all 233 CASE arms | 7.3 to 7.5 |

Moving the marker alternative first, or spelling it `REDACTED_[a-z_]*(key|token|literal)`, did not help (12.0 to 12.7 s). The extra CASE arms cost nothing measurable.

The shipped detector costs 24 to 33% more per derived file than migration 138.
It runs in the content_files trigger on every changed write and in the
bootstrap finalizer. At the measured rate this is about 0.2 ms more per file.
The the QA environment figure is not measured here: migration 138 recorded 0.27 ms per file
on the QA environment, so the shipped detector would scale to about 0.35 ms there.

Lock window: migration 234 is a single `CREATE OR REPLACE FUNCTION`. It takes no
table lock, and writes keep running. A lock-and-re-derive in one file is refused
by the #7961 migration-immutability gate. Re-deriving existing
`content_file_secret_lines` rows is a separate decision, pending a coordinator
ruling. For reference, a single-transaction re-derive measured 160.9 s of
INSERT over the 150k files: it held `FOR SHARE` on every row and wrote 109 MB of
WAL for the row locks.

No-Regression Evidence: the per-file policy lookup that every repository now
pays (the default registry binds two definitions), measured with
`BenchmarkContentStoreGovernedRegistry` over 5,000 ungoverned files:

| Stage | Before | After |
| --- | --- | --- |
| snapshot stage | 1.11 ms, 5,001 allocations | 0.92 ms, 1 allocation |
| meta stage | 1.12 ms, 5,000 allocations | 0.90 ms, 0 allocations |

The improvement comes from `ContentStoreForPath` resolving through a
non-cloning `lookupPath`.

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

Findings differential: `eshu_secret_line_findings` from migration 234 over the
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
