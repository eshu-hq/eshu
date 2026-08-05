# #5703 golden proof: Git workflow image evidence crosses into a CI run

## What the old corpus missed

The accepted golden-corpus gate (B-7) run recorded for #5770 finished with 522
passes, no required failures, and one timing advisory. That run did not test
#5703. The `container-ci-lineage` repository fixture contained only a
Dockerfile, so it emitted zero Git-sourced `ci.workflow_image_evidence` facts.
Its OCI registry cassette, the checked-in collector recording used by this
credential-free run, also had no tag observation for the image named by a
workflow.

That let the gate stay green while Git workflow evidence remained isolated in
a repository scope and GitHub Actions run 9100 remained in its separate
`ci_cd_run` scope.

## Production path

Git remains the owner of static workflow evidence. The CI collector continues
to own provider run observations and does not re-read repository files. The
correlation handler now extracts the canonical repository IDs from its typed
`ci.run` anchors, loads active Git workflow facts for those owners, and appends
them before it derives the image references used by the existing bounded
container-identity load.

The Postgres bridge uses `ingestion_scopes.partition_key` as the repository
owner boundary. It accepts active default and explicit-ref Git scopes, requires
the scope and generation to be active, excludes tombstones, orders by
`(observed_at, fact_id)`, and reads one row beyond a 12,000-fact safety cap so
overflow fails closed. The reducer decodes the returned rows again, drops
foreign owners, wrong fact kinds, and duplicate fact IDs, and retains malformed
rows for the existing typed quarantine or fatal-schema handling.

Git delta generations carry a complete current workflow-evidence snapshot.
Unchanged workflows use a body-free extraction-only lane, changed workflows use
the ordinary delta path, and deleted workflows are absent from the next active
generation. A deleted workflow's real collector output is a generic `file`
tombstone, so the projector recognizes only direct
`.github/workflows/*.yml|yaml` tombstones as identity triggers. The existing
durable `container_image_identity -> ci_cd_run_correlation` completion edge then
reopens the current CI decision after the Git generation becomes active.

## Theory and performance proof

The owner-indexed SQL shape was measured before implementation on Postgres 16
with 500,000 unrelated active Git scopes and 500,000 workflow facts. It used the
existing `ingestion_scopes_source_idx` and
`fact_records_scope_generation_idx`; no new index or migration is required.

| Query | Execution | Shared buffers | Plan shape |
| --- | ---: | ---: | --- |
| Owner-indexed candidate, cold | 4.605 ms | 24 | indexed scope and fact lookups |
| Owner-indexed candidate, warm | 0.185 ms | 24 | indexed scope and fact lookups |
| Payload-only repository scan | 43.479 ms | 14,222 | parallel scan of 500,000 facts |

The first complete-delta prototype routed every unchanged workflow through the
full parser and was rejected: 100 workflows increased the measured generation
time from 254.4 ms to 339.2 ms and added 3.90 MB across 36,420 allocations. The
accepted extraction-only lane measured 47.3 ms for the same 100-workflow shape;
relative to the rejected route, incremental heap fell 84.8% and incremental
allocations fell 89.7%. The 0/1/5/10/100-workflow medians and exact benchmark
command are retained in `go/internal/collector/OPERATIONS.md`.

The real-Postgres forced-order proof starts with a succeeded CI correlation that
cannot yet see Git workflow evidence. Activating the workflow evidence and
acknowledging its identity work emits one durable completion event, rejects a
duplicate ACK, reopens only the current CI generation, and converges on replay
to one canonical `workflow_image` decision. A second completion-runner pass is
empty. This proves both activation orders without serializing collectors or
reducers.

## Fixture and saved-answer contract

The repository fixture now contains
`.github/workflows/build-image.yml`. The real Git collector streaming path reads
that file and emits one observed workflow-image fact for
`ghcr.io/acme/container-ci-lineage:1.0.0`. No cassette hand-authors that fact.

The existing CI cassette still owns GitHub Actions run 9100 and its artifact
digest. One new OCI registry tag observation maps the workflow tag to that same
digest. The B-12 saved-answer snapshot then asks the HTTP API for the one row
with all of these values on the same object:

- provider `github_actions`
- repository `repository:r_19519f37`
- run `9100`
- correlation kind `workflow_image`
- image reference `ghcr.io/acme/container-ci-lineage:1.0.0`
- artifact digest `sha256:c10000000000000000000000000000000000000000000000000000000010c1c1`
- canonical writes `1`
- outcome `derived`

The fixture staging directory has no `.git` metadata, so the workflow fact has
no `commit_sha`. The reducer must use its repository-wide fallback, which is why
the expected outcome is `derived`. The query requires exactly one row. Removing
the workflow file makes collection emit no matching fact; removing the
cross-scope load makes the query empty; breaking tag resolution leaves
`canonical_writes=0` and an empty digest; publishing duplicate current rows
breaks the one-row ceiling.

## Focused red and green checks

The collector regression test failed before the workflow file existed:

```text
go test ./internal/collector -run '^TestContainerCILineageFixtureEmitsWorkflowImageEvidence$' -count=1

workflow image fact count = 0, want 1
exit_code=1
```

After adding the real fixture file, the same production streaming path passed:

```text
ok github.com/eshu-hq/eshu/go/internal/collector
exit_code=0
```

The saved-answer and tag-resolution tests also failed first, with the HTTP shape
missing and zero matching tag observations. Both focused tests now pass:

```text
go test ./cmd/golden-corpus-gate -run '^TestGoldenSnapshotPinsContainerCILineageWorkflowImageCorrelation$' -count=1
ok github.com/eshu-hq/eshu/go/cmd/golden-corpus-gate

go test ./cmd/golden-corpus-gate -run '^TestGoldenOCICassetteResolvesContainerCILineageWorkflowTag$' -count=1
ok github.com/eshu-hq/eshu/go/cmd/golden-corpus-gate
```

PR review found that the cross-scope ownership fence dropped a malformed
workflow fact before the typed classifier could quarantine it. A regression
fixture with `repository_id` absent reproduced the silent loss:

```text
go test ./internal/reducer \
  -run '^TestCICDRunCorrelationHandlerBridgesGitWorkflowImagesByRunRepository$' \
  -count=1

input_invalid_facts = 0, want 1 because malformed bridge rows must reach quarantine
exit_code=1
```

The fence now retains decode failures for the existing per-fact quarantine or
fatal-schema path while continuing to reject successfully decoded foreign
owners, wrong fact kinds, and duplicate fact IDs. The focused test, full
reducer suite, and focused race run all pass after that change.

## Full B-7 result

The orchestrator ran the complete credential-free pipeline after the reducer,
storage, fixture, cassette, and saved-answer changes were in place:

```text
ESHU_POSTGRES_PORT=25432 NEO4J_BOLT_PORT=27687 NEO4J_HTTP_PORT=27474 \
  GATE_API_PORT=28080 GATE_MCP_PORT=28091 \
  bash scripts/verify-golden-corpus-gate.sh

summary: 523 pass, 0 required-fail, 1 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 141s, budget ceiling 1800s) ===
exit_code=0
```

The one advisory was the existing maintenance-drain timing check: 30 seconds
against a 19-second advisory ceiling. The required pipeline wall-time check
passed at 2 minutes 21 seconds against its 30-minute ceiling. All first-pass,
maintenance, and final drains reported zero residual fact work, zero required
nonterminal projection intents, and zero nonterminal cross-scope completion
events.

No-Observability-Change: The fixture, cassette, snapshot, and focused tests add
no runtime metric, log label, worker, or queue. The production handler records
the bounded bridge time in the existing reducer result as
`workflow_image_bridge_load`; existing reducer duration, decision counters,
queue state, completion-event state, and structured failure logs retain the
operator path without repository IDs in metric labels.
