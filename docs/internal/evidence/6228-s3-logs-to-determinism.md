# #6228 s3_logs_to determinism evidence

TDD slice for the s3_logs_to family (WriteS3LogsToEdges -> LOGS_TO),
mirroring the ec2_uses_profile slice: offline Odu (13 facts, uniqueness RED
then GREEN) + hand-derived expected-edge fixture + guard kind partition
(resolution_mode=name pinned) + registry row 19 + cassette projection +
drive/assert fns + post-delta assert + determinism/fault mirrors.

The fixture: 7 scanned S3 bucket node facts (five endpoints, one
scanned-with-no-posture idle control, one ARN-only node) + 6 posture facts:
two name-resolved edge producers (orders->logs, audit->audit self-target
which DOES emit), one ARN-only producer proving the ARN-tail name fallback,
one logging-disabled blank target (no edge, not a skip), one ghost target
(target_unresolved skip), one orphan posture with no node
(source_unresolved skip).

The 3 edge uids were derived by an independent recomputation (python
sha256 over the StableID sorted-key JSON, cross-checked against
payloadcore.CloudResourceUID/facts.StableID source), then confirmed by the
guard running the REAL extractor: coverage GREEN with zero MISSING/EXTRA.

## Live N=1/2/4 determinism drive (2026-09-28)

`NORNICDB_IMAGE=ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c
bash scripts/verify-ifa-determinism.sh --keep` (tag form; the digest form
fails with "cannot overwrite digest", same workaround as prior slices)
drove all three cells to their graph dumps with 0 failed asserts:

- All three canonical dumps are byte-identical:
  `graph-n{1,2,4}.dump` sha256
  `d0559cfe5cc248133e00d0f1f769dda8c8aa2bd61095d9b79f713567672b7312`
  (1,613,681 bytes each) — the determinism comparison itself, measured
  directly rather than via the gate's digest line.
- Terminal counts: 13 committed cassette facts under the new scope (7
  `aws_resource` S3 bucket nodes + 6 `s3_bucket_posture` facts) with
  `generations_committed=1` at workers=1, workers=2, and workers=4;
  exact three-edge `LOGS_TO` set per run (pre-delta via the registry
  loop, post-delta via the pinned chain); 0 failed asserts
  (49 `[PASS]` lines, `grep -c FAIL` = 0 on the gate log). The
  within-run control is the sibling set: the same script on the same
  stack without this wiring drives everything but s3, and the after
  measurement adds the driven family while every sibling assert stays
  green. Kept workdir:
  `$TMPDIR/ifa-determinism.XXXXXX.Md4DmdOMDo` (dumps, rationale deltas,
  per-cell drive logs; N=1 wall 204s, N=2 204s, N=4 207s).
- Gate log: /tmp/s3-live.log (retained on the driving machine only).

## No-Regression

No-Regression Evidence: the writer file edit
(go/internal/storage/cypher/s3_logs_to_edge_writer.go)
is const-export renames ONLY (canonicalS3LogsToEdgeUpsertCypherFormat,
retractS3LogsToEdgesCypher -> exported spellings, plus their doc comments);
the upsert/retract Cypher text is byte-identical, no statement shape,
predicate, or plan change. `verify-query-plan-regression` is run singly
before push (DEFER-CI surface: writer file touched). Full offline proof:
familyodu + materializededges + full ifa tree + cypher tree GREEN,
cassette-author gate EXIT 0 (bucket names carry no dots/TLDs, account is
the 123456789012 documentation form), determinism + fault-injection
mirrors GREEN, gofmt + git diff --check clean. No baseline exists for
this family (first drive); the sibling-set within-run control above is
the regression control.

## No-Observability-Change

No-Observability-Change: no new spans, metrics, logs, or status surface.
The slice wires an existing writer, existing fact kinds, and existing gate
machinery. The determinism gate's existing PASS/FAIL lines cover the new
asserts.
