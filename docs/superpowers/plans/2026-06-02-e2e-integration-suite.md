# E2E Integration Suite Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a full E2E integration suite that validates collectors, reducers, HTTP API, MCP, CLI, restart behavior, and rollout evidence before release or EKS claims.

**Architecture:** Define one public-safe evidence manifest first, then have remote Compose, readback parity, and Kubernetes gates emit the same envelope. Keep collectors as source-fact emitters, reducers as truth owners, and API/MCP/CLI as bounded readers.

**Tech Stack:** Go, shell scripts, Docker Compose, Kubernetes/kubectl, Helm, Postgres, NornicDB, Eshu API, Eshu MCP, OTEL/pprof, MkDocs.

---

## Chunk 1: Evidence Manifest And Corpus Contract (#1230)

### Task 1: Locate Existing Gate Shapes

**Files:**
- Read: `scripts/security_intelligence_release_gate.sh`
- Read: `scripts/verify_remote_e2e_runtime_state.sh`
- Read: `docs/internal/design/1225-e2e-integration-suite.md`
- Read: `docs/public/reference/security-intelligence-release-gate.md`

- [ ] **Step 1: Read the current release-gate scripts**

Run:

```bash
sed -n '1,260p' scripts/security_intelligence_release_gate.sh
sed -n '1,260p' scripts/verify_remote_e2e_runtime_state.sh
```

Expected: identify current state, runtime, readback-proof, proof-matrix, and k8s evidence fields.

- [ ] **Step 2: Map reusable fields**

Write a short note in the PR body mapping current fields to the new E2E manifest fields.

### Task 2: Add Manifest Fixture Tests

**Files:**
- Create: `scripts/test-e2e-evidence-manifest.sh`
- Create or modify: `scripts/lib/e2e_evidence_manifest.sh`

- [ ] **Step 1: Write failing tests for missing required evidence**

Add cases that fail when collector, reducer, API, MCP, CLI, pprof, logs, queue, or corpus coverage is missing.

Run:

```bash
scripts/test-e2e-evidence-manifest.sh
```

Expected: FAIL because the manifest validator does not exist yet.

- [ ] **Step 2: Write failing tests for privacy rejection**

Add cases with repository-looking strings, package-looking keys, URLs, hostnames, paths, tokens, account ids, and provider payload fields.

Run:

```bash
scripts/test-e2e-evidence-manifest.sh
```

Expected: FAIL with privacy rejection missing.

- [ ] **Step 3: Implement minimal validator**

Implement a shell or Go-backed validator that accepts schema version `1`, required status enums, required counts, and public-safe aggregate fields.

- [ ] **Step 4: Run focused tests**

Run:

```bash
bash -n scripts/test-e2e-evidence-manifest.sh scripts/lib/e2e_evidence_manifest.sh
scripts/test-e2e-evidence-manifest.sh
```

Expected: PASS.

### Task 3: Document The Manifest

**Files:**
- Modify: `docs/public/reference/local-testing/remote-collector-e2e.md`
- Modify: `docs/public/reference/security-intelligence-release-gate.md`

- [ ] **Step 1: Document schema and corpus expectations**

Document the public-safe fields, representative corpus coverage, skip states, and failure classes.

- [ ] **Step 2: Verify docs**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Expected: PASS.

- [ ] **Step 3: Commit**

Commit message:

```bash
git commit -m "Add E2E evidence manifest contract"
```

## Chunk 2: Remote Compose Collector And Reducer Harness (#1227)

### Task 1: Add Harness Fixture Tests

**Files:**
- Create: `scripts/test-e2e-remote-compose-suite.sh`
- Create or modify: `scripts/e2e_remote_compose_suite.sh`

- [ ] **Step 1: Write failing tests for clean-volume proof**

The fixture should require clean backing-store evidence, pprof URL, docker stats, logs, queue state, workflow state, and required collector summaries.

Run:

```bash
scripts/test-e2e-remote-compose-suite.sh
```

Expected: FAIL because the harness does not exist.

- [ ] **Step 2: Write failing tests for preserved-volume proof**

The fixture should require previous clean evidence, restart-without-prune state, same backing-store proof, no duplicate claim/fact/finding counters, and zero new dead letters.

Expected: FAIL until implemented.

### Task 2: Implement Remote Harness Wrapper

**Files:**
- Create or modify: `scripts/e2e_remote_compose_suite.sh`
- Modify only if needed: `scripts/verify_remote_e2e_runtime_state.sh`

- [ ] **Step 1: Implement clean run collection**

Call existing runtime-state verifier, collect docker stats, pprof, logs, fact counts, workflow summaries, reducer summaries, and manifest validation.

- [ ] **Step 2: Implement preserved restart collection**

Accept previous clean evidence and preserved volume proof; fail on duplicate outputs or new retry/dead-letter state.

- [ ] **Step 3: Run focused tests**

Run:

```bash
bash -n scripts/e2e_remote_compose_suite.sh scripts/test-e2e-remote-compose-suite.sh
scripts/test-e2e-remote-compose-suite.sh
```

Expected: PASS.

### Task 3: Prove Remotely

**Files:**
- No public private-env changes.

- [ ] **Step 1: Start representative remote Compose**

Run on the remote host with private env:

```bash
docker compose --env-file "$ESHU_REMOTE_E2E_ENV_FILE" \
  -f docker-compose.remote-e2e.yaml \
  -f docker-compose.remote-e2e.pprof.yaml \
  up --build
```

Expected: stack reaches representative acceptance state.

- [ ] **Step 2: Run clean harness**

Run:

```bash
scripts/e2e_remote_compose_suite.sh \
  --run-kind clean \
  --manifest /secure/local/e2e-manifest.json \
  --api-base-url "$REMOTE_API_BASE_URL" \
  --pprof-base-url "$REMOTE_PPROF_BASE_URL"
```

Expected: PASS, public-safe evidence only.

- [ ] **Step 3: Restart without pruning volumes and run preserved harness**

Expected: PASS, no duplicate claims/facts/findings, no new dead letters.

- [ ] **Step 4: Commit**

Commit message:

```bash
git commit -m "Add remote Compose E2E suite"
```

## Chunk 3: API, MCP, And CLI Readback Parity (#1226)

### Task 1: Define Readback Groups

**Files:**
- Create: `scripts/test-e2e-readback-parity.sh`
- Create or modify: `scripts/e2e_readback_parity.sh`

- [ ] **Step 1: Add route/tool group fixtures**

Cover repository, package, cloud, deployment, vulnerability, SBOM/image, observability, incident, work-item, service, and status domains.

- [ ] **Step 2: Add parity failure fixtures**

Fail when API passes but MCP is missing, MCP passes but CLI differs, empty results lack ready state, limits are absent, or private data appears in aggregate evidence.

### Task 2: Implement Readback Parity Runner

**Files:**
- Create or modify: `scripts/e2e_readback_parity.sh`

- [ ] **Step 1: Query API with limits and timeouts**

Use bounded calls and record checked, failed, truncated, unsupported, missing-evidence, and ambiguity counts.

- [ ] **Step 2: Query MCP tools**

Use the configured MCP endpoint or local client wrapper. Compare truth, readiness, missing evidence, and counts against API.

- [ ] **Step 3: Query CLI commands**

Only include CLI commands that are expected to read through API or the same envelope.

- [ ] **Step 4: Emit release-gate readback-proof JSON**

Output an aggregate file accepted by `scripts/security_intelligence_release_gate.sh --phases readback-proof`.

- [ ] **Step 5: Run focused tests**

Run:

```bash
bash -n scripts/e2e_readback_parity.sh scripts/test-e2e-readback-parity.sh
scripts/test-e2e-readback-parity.sh
```

Expected: PASS.

- [ ] **Step 6: Commit**

Commit message:

```bash
git commit -m "Add API MCP CLI E2E readback parity"
```

## Chunk 4: Kubernetes E2E Proof Gate (#1229)

### Task 1: Add Kubernetes Fixture Tests

**Files:**
- Create: `scripts/test-e2e-k8s-suite.sh`
- Create or modify: `scripts/e2e_k8s_suite.sh`

- [ ] **Step 1: Write fake kubectl/helm tests**

Cover pods, resources, logs, ServiceMonitor, NetworkPolicy, PDB, Helm values, bootstrap job health, queue readback, and pprof reachability.

Expected: FAIL before implementation.

### Task 2: Implement K8s Suite

**Files:**
- Create or modify: `scripts/e2e_k8s_suite.sh`

- [ ] **Step 1: Capture sanitized Kubernetes evidence**

Summarize pods, resource requests/limits, restart counts, logs, Helm values, ServiceMonitor, NetworkPolicy, PDB, and bootstrap jobs.

- [ ] **Step 2: Run API/MCP readback parity**

Call the Chunk 3 readback runner against the Kubernetes API/MCP endpoints.

- [ ] **Step 3: Run manifest validation**

Use the Chunk 1 manifest validator and fail closed for missing evidence.

- [ ] **Step 4: Run focused tests**

Run:

```bash
bash -n scripts/e2e_k8s_suite.sh scripts/test-e2e-k8s-suite.sh
scripts/test-e2e-k8s-suite.sh
```

Expected: PASS.

### Task 3: Prove Against Staging EKS

**Files:**
- No public private-env changes.

- [ ] **Step 1: Capture staging proof**

Run with private kube context and private port-forwards. Do not commit private endpoints.

- [ ] **Step 2: Record public-safe evidence**

Attach only aggregate public-safe evidence to the PR or issue.

- [ ] **Step 3: Commit**

Commit message:

```bash
git commit -m "Add Kubernetes E2E proof gate"
```

## Final Verification

- [ ] Run all focused E2E suite tests.
- [ ] Run docs build if docs changed.
- [ ] Run `scripts/test-verify-performance-evidence.sh`.
- [ ] Run `scripts/verify-performance-evidence.sh`.
- [ ] Run `git diff --check`.
- [ ] Confirm public evidence contains no private source data.
- [ ] Create or update follow-up GitHub issues for any missing evidence found.

