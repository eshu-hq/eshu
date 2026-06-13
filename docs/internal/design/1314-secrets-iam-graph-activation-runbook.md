# Secrets/IAM Graph Projection — Flag-On Activation Runbook

Issue [#1381](https://github.com/eshu-hq/eshu/issues/1381) / [#1347](https://github.com/eshu-hq/eshu/issues/1347). Gate: ADR #1314 §11 / §12 / §14.

This runbook is the activation procedure a principal/security reviewer and an
operator follow to enable the default-off Secrets/IAM graph projection in one
target deployment. The implementation, the §11 fixture proof, the §12 writer
benchmark, the NornicDB + Neo4j live conformance, and the §14 principal/security
sign-off are **already done and recorded**. This page covers **only the
remaining activation steps**.

This runbook does not grant any approval and does not enable anything. It
captures the steps and evidence that activation requires. Setting
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` truthy before the
`risk:schema` approval and the target-deployment decision below are recorded is
a rule violation, not a config choice.

## Gate status (resolved, 2026-06-13) — issue #2349

The gate is **explicitly OFF by default and tracked**, not silently dark. The
remaining blocker to enable is **human, not code**: the §2 `risk:schema`
activation approval and the §3 single-target-deployment decision. Until a
principal/security reviewer records those, the writer stays `nil` and
`DomainSecretsIAMGraphProjection` stays unregistered. No agent or config change
may flip it; doing so before §2/§3 are recorded is a rule violation (§7).

**What this flag gates — and what it does not.** This flag gates **only**
`DomainSecretsIAMGraphProjection`: the projection of reducer-owned
identity-trust-chain and secret-access-path facts into the `SecretsIAM*` graph
node/edge families. It does **not** gate:

- **GCP cloud relationship edges** (`DomainGCPRelationshipMaterialization`, issue
  #2348) — these project `GCP_<TYPE>` edges between `CloudResource` nodes and are
  live independently of this flag.
- **GCP IAM privilege-posture observations** (issue #2347) — these are
  reducer-owned read-model facts (`gcp_service_account_secret_access`,
  `gcp_service_account_broad_role`) surfaced through the secrets-IAM query
  surface and the posture-observations counter, not graph writes behind this
  flag.

So a GCP shop already gets GCP inventory, GCP relationship edges, and GCP IAM
posture with this flag off. What stays dark until the flag is enabled is the
graph-projected **trust chain** (the `SecretsIAM*` node/edge families) — for AWS
(IRSA/Pod-Identity → Vault) today, and for GCP once the GCP
impersonation/Workload-Identity trust layer (issue #2369) lands. GCP
graph-projected identity hops therefore require **both** #2369 and this flag;
GCP IAM posture and GCP relationship edges require neither.

**Blocking item tracked:** the §2 `risk:schema` approval + §3 target-deployment
decision remain open (see the proof snapshot "Still blocked"). They are the
explicit, owner-gated steps that move this flag from documented-off to enabled.

## Why this gate exists

The projection writes a live Secrets/IAM graph (four `SecretsIAM*` node
families, the resolvable `SECRETS_IAM_*` edges) into the canonical backend. Per
ADR §7 those rows are metadata-only: join keys, fingerprints, bounded enums, and
scope metadata. The reducer never persists raw secret material, ARNs, or Vault
paths. Flag-on means real exact-only graph writes start flowing, so activation
is gated behind explicit human approval and a live activation proof, not behind
merged code or a green pod.

## 1. Preconditions

Activation may begin only when all of the following are already true and
recorded. Cite each against its source.

- **ADR §14 principal/security sign-off recorded** — recorded approved
  2026-06-07 in `docs/internal/design/1314-secrets-iam-graph-promotion-proof-2026-06-07.md`.
- **§11 fixture truth proven** — reducer read-model rows drive node and edge
  rows through `SecretsIAMGraphProjectionHandler`
  (`go/internal/reducer/secrets_iam_graph_projection_fixture_truth_test.go`).
- **§12 writer benchmark recorded** — `BenchmarkSecretsIAMGraphWriter` proves
  the UNWIND-batched, uid-anchored write shape with no per-edge reads
  (`go/internal/storage/cypher/secrets_iam_graph_writer_bench_test.go`).
- **Backend conformance proven on both backends** — NornicDB and Neo4j both
  passed `TestSecretsIAMGraphWriterLiveConformance`
  (`go/internal/storage/cypher/secrets_iam_graph_live_test.go`) and the shared
  backend conformance script.
- **Schema readback confirmed** — the four `SecretsIAM*` uid constraints plus
  scope indexes are present after bootstrap (proof snapshot).
- **§7 redaction allowlist enforced in code** — the property allowlist is
  asserted by `TestExtractRowsCarryNoForbiddenProperties`
  (`go/internal/reducer/secrets_iam_graph_projection_extract_test.go`); any key
  outside the allowlist, or a value that looks like a raw ARN/path, fails the
  test.

If any precondition is not recorded, stop. Do not proceed.

## 2. `risk:schema` activation approval

Still blocked per the proof snapshot ("Still blocked") and the gate doc. This is
a separate approval from §14.

- **Who records it:** the principal/security reviewer responsible for the
  schema/risk surface.
- **What they attest:**
  1. The §1 preconditions are all present and current for this deployment.
  2. The metadata-only redaction contract (§7) is the one enforced by
     `TestExtractRowsCarryNoForbiddenProperties`, and no code change has widened
     the allowlist since the §14 sign-off.
  3. The four `SecretsIAM*` uid constraints and scope indexes exist in the
     **target** backend, not only in the proof stacks.
  4. They approve schema-level activation for the single target deployment named
     in §3.
- **Where:** record the `risk:schema` approval alongside the §14 record (same
  proof-snapshot family), with reviewer, date, and the target-deployment
  identifier. Do not enable the flag until this line exists.

## 3. Target-deployment decision

Record an explicit decision before any flag flip. Activate **one** deployment at
a time.

- **Deployment:** name exactly one target (placeholder: `<target-deployment>`).
  No fleet-wide enablement.
- **Backend:** declare NornicDB (default canonical) or Neo4j (compatibility).
  Both have recorded conformance; the decision pins which one this deployment
  runs.
- **Scope:** confirm the reducer scope(s) the projection will write and retract.
  The writer scopes every retract by its own `scope_id` + `evidence_source`, so
  it only ever removes its own rows; confirm no other writer owns the
  `SecretsIAM*` labels in this deployment.
- **Rollback owner + window:** name who holds rollback (§6) and the observation
  window after flip.

## 4. Enable procedure

The flag is read in `go/cmd/reducer/secrets_iam_graph_wiring.go`:

- **Env var:** `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`
- **Truthy values:** parsed by `strconv.ParseBool` (`1`, `t`, `true`, and their
  case variants). Empty is OFF. A malformed value is a hard error at reducer
  start, so a typo fails fast rather than silently reading as either state.

**Registry-gate behavior (read before flipping):**

- OFF (default, empty): the projection writer is a true `nil`; the additive
  registry gate leaves `DomainSecretsIAMGraphProjection` **unregistered**. No
  live graph write happens.
- ON: the live `SecretsIAMGraphWriter` is constructed, the domain registers, and
  the reducer logs a startup `Warn` that live exact-only graph writes are
  active. Treat that log line as the operator's flag-on confirmation.

**Where to set it (Helm):** the reducer runs as the `resolutionEngine`
deployment. Set the var on its `env` map in your deployment values (do not edit
the chart default `resolutionEngine.env: {}` in `deploy/helm/eshu/values.yaml`):

```yaml
resolutionEngine:
  enabled: true
  env:
    ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED: "true"
```

Apply via your normal upgrade path and confirm the reducer pod restarted and
emitted the flag-on `Warn` line.

## 5. Flag-on live activation proof to capture

Capture this before declaring activation complete. The §11/§12 proofs were
against the standalone writer; this proves the live path in the target backend.

**a. Live writer conformance against the target backend.** Run the
backend-gated live test with the env shape from `secrets_iam_graph_live_test.go`.
For NornicDB:

```bash
cd go
ESHU_SECRETS_IAM_GRAPH_LIVE=1 \
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_URI=bolt://<target-bolt-host>:<port> \
ESHU_NEO4J_USERNAME=<user> \
ESHU_NEO4J_PASSWORD=<secret> \
ESHU_NEO4J_DATABASE=nornic \
go test ./internal/storage/cypher -run '^TestSecretsIAMGraphWriterLiveConformance$' -count=1 -v
```

For Neo4j, set `ESHU_GRAPH_BACKEND=neo4j` and the Neo4j database/URI. The test
writes the node families and edges, reads them back, then proves scoped retract
removes only the reducer-owned `SecretsIAM*` rows while leaving retained
endpoints intact. Without `ESHU_SECRETS_IAM_GRAPH_LIVE` and a Bolt backend it
skips cleanly — a skip is **not** a pass; capture the `PASS` line.

**b. §7 redaction-allowlist end-to-end no-regression.** Re-run the allowlist
enforcement and focused projection gate to prove no property has leaked past the
allowlist on the exact commit being deployed:

```bash
cd go
go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer \
  -run 'SecretsIAMGraph|SecretsIAM|TestExtractRowsCarryNoForbiddenProperties' \
  -count=1
```

`TestExtractRowsCarryNoForbiddenProperties` is the §7 gate: it fails if any row
carries a key outside the allowlist or any value that looks like a raw ARN/path.

**c. Live readback in the target deployment.** After the flag-on pod is running
and a generation has projected, confirm in the target backend that the
`SecretsIAM*` nodes/edges appear under the reducer's `scope_id` +
`evidence_source` and carry only allowlisted properties. Spot-check that no
node/edge property contains a raw ARN, secret value, or Vault path.

**Evidence to attach** to the activation record: the target-deployment
identifier and backend; the live conformance `PASS` line; the focused gate pass
output; the reducer flag-on `Warn` log line; and the live readback counts under
the reducer scope. Do not claim activation on explanation alone.

## 6. Rollback

To disable:

1. Remove or set `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` to
   empty/false on `resolutionEngine.env` and roll the reducer.
2. On restart the writer is `nil`, `DomainSecretsIAMGraphProjection` is
   unregistered, and the flag-on `Warn` line no longer appears. **No further
   live `SecretsIAM*` graph writes or retracts occur.**

What rollback leaves behind: the `SecretsIAM*` nodes/edges already written
**remain in the backend** — disabling the flag stops new writes, it does not
retract existing rows. If the §3 decision requires removing them, run a scoped
retract through the writer over the reducer's `scope_id` + `evidence_source`
before teardown, then re-verify the reducer-owned label counts are 0 while the
retained endpoints survive. The four `SecretsIAM*` uid constraints and scope
indexes also remain; leaving them is harmless and lets a later re-enable skip
re-bootstrap.

## 7. What NOT to do

- **Do not enable before approvals are recorded.** No flag flip until both the
  §14 sign-off and the §3 `risk:schema` approval + target-deployment decision
  are recorded.
- **Do not widen or bypass the §7 property allowlist.** Never add a property key
  to the extract output to "carry more context." The allowlist (enforced by
  `TestExtractRowsCarryNoForbiddenProperties`) is the redaction contract; any
  change to it re-opens the §14/`risk:schema` review.
- **Do not reach for a serialization workaround.** The writer is UNWIND-batched,
  uid-anchored, and idempotent under concurrent delivery. Do not "fix" a
  perceived write race by dropping batch size to 1, single-threading the
  reducer, or disabling concurrent writers; redesign per the conflict-key model
  instead.
- **Do not fabricate the live proof.** A skipped live test is not evidence.
  Capture the real `PASS` against the target backend.
- **Do not enable more than one deployment per activation record.** Each target
  gets its own §3 decision and §5 proof.
