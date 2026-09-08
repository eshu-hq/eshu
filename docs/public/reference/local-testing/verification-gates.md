# Verification Gates

Use these after selecting the smallest proof from
[Local Testing](../local-testing.md). Run long Compose gates one at a time;
many allocate local ports and reuse Compose project state.

## Go Runtime Package Gate

Use this broad package gate for runtime and collector wiring:

```bash
cd go
go test ./internal/parser/... ./internal/collector/discovery ./internal/content/shape \
  ./internal/collector ./cmd/collector-git ./cmd/collector-terraform-state \
  ./cmd/collector-aws-cloud \
  ./cmd/ingester ./cmd/bootstrap-index \
  ./internal/runtime ./internal/app ./internal/telemetry \
  ./internal/storage/cypher ./internal/storage/neo4j ./internal/storage/postgres \
  ./internal/projector ./internal/reducer ./cmd/reducer -count=1
```

## Replatforming API/MCP Parity Proof

Run this gate when changing any replatforming serving surface: the plan
(`POST /api/v0/replatforming/plans` / `compose_replatforming_plan`),
ownership-packet
(`POST /api/v0/replatforming/ownership-packets` / `find_unmanaged_resource_owners`),
or rollup
(`POST /api/v0/replatforming/rollups` / `get_replatforming_rollups`) route or
tool, or their shared source-state, safety-gate, or readiness logic.

```bash
cd go
go test ./internal/mcp -run TestReplatforming -count=1
```

This is an in-process, fixture-backed proof. It mounts one `query.IaCHandler`
over a deterministic IaC-management fixture store and drives each request twice:
once straight through the HTTP route and once through the real MCP dispatch path
(`dispatchTool` → `resolveRoute` → the same mounted handler →
`parseCanonicalEnvelope`). It then asserts:

- **API/MCP parity** — for one scope the HTTP route and the MCP tool return the
  identical canonical envelope `Data` block plus identical truth label (level,
  basis, capability, freshness). Full-`Data` equality pins bounded results,
  source-state totals, readiness counts, refusal reasons, and stories to one
  contract, so the two surfaces cannot diverge silently.
- **Refusal safety** — safety-gated findings (`security_review_required`,
  ambiguous, stale, unknown) resolve to the `rejected` source state and a
  refused import candidate with reasons; they are never silently omitted nor
  counted as import-ready. Only a safety-approved `cloud_only` finding with a
  supported import mapping is import-ready.
- **Profile/truth bounds** — an unsupported runtime profile returns
  `unsupported_capability` on both surfaces instead of a downgraded answer, and
  neither surface leaks a confident truth level on the refusal path.
- **Negative leakage** — a credential-shaped raw tag value never appears in
  either surface's serialized payload.

The fixture proof is the deterministic CI gate. The operator-facing complement
is the remote all-collector Compose proof in
[Remote collector E2E](remote-collector-e2e.md): once a representative or
full-corpus stack has drained AWS runtime drift, drive the same three routes and
tools against the live API and MCP server and compare the bounded payloads,
truth labels, source-state counts, readiness counts, and refusal summary for the
same scope. The Compose proof records fact counts, queue and dead-letter state,
and the safety/refusal summary; this in-process gate proves the surfaces agree
before that run.

No-Observability-Change: this gate exercises the existing replatforming query
spans and truth envelopes; it adds no metric, span, or log. Runtime diagnosis of
the same surfaces still uses the `query` handler spans named in
`go/internal/telemetry/contract.go` and the canonical response envelope.

## Collector Gates

Use focused checks when changing collector families or source providers.
Tracked evidence must name input size, fact count, wall time, API budget, and
telemetry.

```bash
cd go

go test ./internal/collector/terraformstate -count=1 -run TestParseStream_PeakMemoryGate

go test ./internal/collector/awscloud/awsruntime \
  -run 'TestClaimedSourceRecordsEmissionCounters|TestClaimedSourceRecordsScanStatusWithAPICallStats' \
  -count=1 -v

go test ./internal/collector/ociregistry/ociruntime \
  -run 'TestSourceNextEmitsCollectedGenerationForRegistryTarget|TestClaimedSourceNextClaimedScansMatchingTargetWithClaimGeneration' \
  -count=1 -v

go test ./internal/collector/packageregistry/packageruntime \
  -run 'TestClaimedSourceParsesMetadataIntoPackageRegistryFacts|TestClaimedSourceTruncatesMetadataOverVersionLimit|TestClaimedSourceSanitizesSourceURIBeforeFactEmission' \
  -count=1 -v

go test ./internal/collector/confluence \
  -run 'TestSourceRecordsBoundedConfluenceMetrics|TestHTTPClientRecordsBoundedRequestMetrics' \
  -count=1 -v
```

Terraform-state parser trend and large-state proof:

```bash
cd go
go test -bench=BenchmarkParseStream_LargeState -benchmem -run=^$ \
  ./internal/collector/terraformstate

ESHU_TFSTATE_100MIB_PROOF=true \
  go test ./internal/collector/terraformstate -count=1 \
  -run TestParseStreamLargeState100MiBStreamingProof -timeout 300s
```

## Local-Authoritative Gates

Before a run that executes local Eshu binaries:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Use these for local-host startup, graph-backed query compatibility, or
NornicDB routing:

```bash
ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

Manual MCP smokes should end with:

```bash
eshu graph stop --workspace-root "$PWD"
eshu graph status --workspace-root "$PWD"
```

The status output should report no active owner for that workspace.

## Compose Gates

Run only the gate that matches the touched behavior. These scripts own fixture
setup and teardown; use them instead of hand-run Compose when you need
acceptance evidence.

```bash
./scripts/verify_collector_git_runtime_compose.sh
./scripts/verify_projector_runtime_compose.sh
./scripts/verify_reducer_runtime_compose.sh
./scripts/verify_incremental_refresh_compose.sh
./scripts/verify_webhook_refresh_compose.sh
./scripts/verify_relationship_platform_compose.sh
./scripts/verify_admin_refinalize_compose.sh
./scripts/verify_graph_analysis_compose.sh
./scripts/verify_correlation_dsl_compose.sh
./scripts/verify-graph-rebuild-from-facts.sh
```

> **Advisory known-bad:** `verify-graph-rebuild-from-facts.sh` **does not
> pass today** on the established-cause failures — the `MATCH`-only `CALLS`
> edge write, the resolver ordering, the `Module` key (the documented known
> state in `docs/internal/evidence/4594-graph-rebuild-from-facts.md`). The
> intermittent `HANDLES_ROUTE`/`RUNS_IN` mismatch keeps its standing caveat
> (evidence item 1b): a regression from #6074/#6085 is not ruled out, so a
> red result there stays a signal until diffed against that breakdown.
> Proposed settlement (#6184; owner sign-off pending on the call #6098 left
> open): the strict bidirectional-identity assertion stays as written, and
> the engine fix behind it (the `MATCH`-only `CALLS` edge write and the
> non-deterministic indexing broken down in
> `docs/internal/evidence/4594-graph-rebuild-from-facts.md`) rides a future
> graph-engine change with live Compose proof, tracked by #6184, rather than
> this gate. Do not treat a red result here as a regression without diffing
> it against that breakdown first.

`verify-graph-rebuild-from-facts.sh` runs the disaster-recovery procedure rather
than a single behavior: it indexes the corpus, snapshots the identity of every
node and edge, wipes the graph volume with Postgres preserved, reapplies graph
schema, rebuilds every scope from the surviving facts, and asserts the rebuilt
identities are the same set. The per-label and per-type counts it prints are for
readability; nothing is asserted on them. It then repeats the rebuild with the
workers killed partway through to show a restart converges. Both passes always
run — a mismatch in the first is reported and the run continues, so the
resumability proof is not lost to it — and the run exits non-zero at the end if
either pass differed. It prints `graph_rebuild_seconds` for the SLO row. Set
`ESHU_DR_SKIP_INTERRUPT=true` to run the timed pass only. Reads the procedure
being tested: [Rebuild the graph from facts](../../operate/graph-rebuild-from-facts.md).

It waits on two queues before comparing: `fact_work_items` and the shared edge
backlog in `shared_projection_intents`. The second one drains on its own worker
cycle and has been measured still running four minutes after the first went
quiet, so a comparison made on the first alone reports edges as missing that were
simply not written yet.

It asserts a bidirectional set difference on node and edge identities, not a
count comparison, so a failure names the exact nodes and edges that are missing
or extra rather than reporting a delta of three. Before comparing, it refuses any
snapshot whose keys cannot tell things apart — empty, null, or nothing but the
separators the identity concatenation added — because such a file diffs clean
against any other such file. `scripts/test-verify-graph-rebuild-from-facts.sh`
exercises that refusal without Docker.

**This gate does not pass today**, and that is a known state rather than a broken
environment. A single rebuild pass under-produces two families — one `CALLS` edge
and part of the `EvidenceArtifact` family — and indexing the same corpus twice
does not produce the same graph, so the snapshot it compares against moves
between runs. Each cause, with counts, is in
`docs/internal/evidence/4594-graph-rebuild-from-facts.md`. Do not treat a red
result here as a regression without diffing it against that breakdown first.

For a no-credential proof of additional lanes against public, unauthenticated
endpoints (CISA KEV, FIRST EPSS, OSV, public npm), use the public-collector
gate. It claim-drives the workflow-coordinator and asserts fact commit, reducer
drain to zero, and API/MCP readback with aggregate-only, public-safe output. See
[Public Collector Proof](public-collector-proof.md).

```bash
./scripts/verify_local_public_collector_proof.sh --check   # no Docker, no network
./scripts/verify_local_public_collector_proof.sh           # live public proof
```

Use `./scripts/verify_product_truth_fixtures.sh` when changing product truth
across graph, evidence, API, MCP, CLI, or cleanup workflows.

## Targeted Graph And Terraform Gates

NornicDB grouped-write probes:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteSafetyProbe -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless-eshu-rollback \
ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteRollbackConformance -count=1 -v
```

Normal laptop runs should leave `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES`
unset.

Terraform provider-schema and relationship extraction:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1
```

Terraform config-vs-state drift:

```bash
bash scripts/verify_tfstate_drift_compose.sh

ESHU_TFSTATE_DRIFT_PROOF_OUT=/tmp/eshu-tfstate-drift-compose-$(date +%Y-%m-%d).md \
  bash scripts/verify_tfstate_drift_compose.sh

bash scripts/verify_tfstate_drift_compose_tier2.sh

bash scripts/verify_tfstate_drift_compose_tier2_v25.sh
```

The Terraform-state scripts use distinct `COMPOSE_PROJECT_NAME` values and
dynamic host ports, so tier 1, tier 2, and v2.5 proofs can run side by side.

Webhook refresh:

```bash
bash scripts/verify_webhook_refresh_compose.sh
```

Expected observability: `webhook_refresh_triggers.status`, existing webhook
decision/store metrics, bounded listener request logs, and ingester Git sync
lifecycle logs.

## Moved-File Reference Guard

Any `go/**` change selects `scripts/verify-moved-file-refs.sh`. It fails a
branch that moves or deletes a Go file and leaves a reference **written as the
full `go/...` path** pointing at the path it vacated:

```bash
bash scripts/verify-moved-file-refs.sh              # attribute against origin/main
bash scripts/verify-moved-file-refs.sh --base <ref> # attribute against <ref>
bash scripts/test-verify-moved-file-refs.sh         # its mirror test
```

This is the residue the #6061 reducer subpackage split kept shipping, and
`verify-doc-citations.sh` cannot see it: that gate only tracks a citation
carrying a `:NNN` line suffix, so a **bare path reference is never tracked at
all**. On `main` before #6525, 53 of 517 distinct `go/internal/reducer/*.go`
paths named in `docs/`, `scripts/`, `specs/` and `go/` resolved to nothing.

The path form is load-bearing and is the gate's main blind spot: it searches for
the vacated path as a fixed string, and every vacated path is `go/`-prefixed, so
a reference written module-root-relative (`internal/reducer/widget.go`) or as a
package-relative shorthand (`reducer/widget.go`) is invisible to it. Both forms
occur in the tree today. Widening the match is tracked separately, because it
widens a blocking gate and needs a repo-scale false-positive measurement first.

The check is scoped to the branch, not the tree: a path is reported only when
it existed at the diff base and does not exist now. That is what keeps it
precise. Inherited debt did not change, so it is not reported, and a deliberate
negative fixture — a path a test asserts is *missing*, such as
`does_not_exist.go`, `no_such_handler_file.go`, the telemetry-coverage ghost
rows, or the cigates selector's synthetic `r.go` — never existed at the base and
so can never trip the gate. Working from the branch's own deleted/renamed set
also keeps it cheap: a branch that moves nothing greps for nothing.

For a rename, git's rename detection supplies the new path, so the failure names
the repoint target. Write the repoint **without** a `:NNN` suffix —
`verify-doc-citations.sh` refuses branch-authored LINE occurrences ("LINE debt
may only decrease"), so a repoint that keeps a line number is rejected outright,
while a path-only reference is accepted and survives later line drift.

A genuinely historical reference — a recorded command transcript, or a dated
evidence note describing the tree as it stood at the time, where repointing
would falsify the record rather than refresh it — goes in
`scripts/moved-file-refs-allowlist.txt` as a `<referencing-file>:<vacated-path>`
line with a comment saying why. An entry exempts one referencing file naming one
vacated path; it does not exempt that path everywhere.

## Runtime Tree Hygiene

The deployable runtime tree is Go-only:

```bash
rg --files . -g '*.py' | rg -v '^(\\./)?tests/fixtures/'
```

Fixture data under `tests/fixtures/` and explicitly offline-only tooling can
still carry Python source when they are not part of the deployable runtime.
