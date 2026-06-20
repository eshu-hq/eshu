# Remote Representative Acceptance

Use this page with
[Remote Collector E2E](remote-collector-e2e.md) when a remote Compose proof
runs in representative corpus mode.

Use [Representative Corpus Suite](representative-corpus-suite.md) for the
issue #3170 corpus tiers, required domains, privacy rules, metrics, and
thresholds that gate reducer-scale implementation.
Use [Scale Benchmark Artifact](scale-benchmark-artifact.md) for the issue #3171
public-safe benchmark result shape, threshold rows, backend matrix, commit SHA,
and before/after evidence requirement.

After a representative stack finishes the required corpus pass, run:

```bash
ESHU_REMOTE_E2E_CORPUS_MODE=representative \
  scripts/verify_remote_e2e_runtime_state.sh
```

The verifier checks service health, runtime safety, and aggregate proof
counters. Smoke and full-corpus modes still require strict queue-zero plus
workflow completion. Representative mode uses a scoped terminal contract
because scheduled collectors remain enabled in the remote Compose profile: the
API status must be `healthy` or `progressing`, `retrying`, `failed`, and
`dead_letter` queue counts must be zero, and workflow coordinator `failed` or
blocked-completeness rows must be zero.

Outstanding, in-flight, pending, `reducer_converging`, and
pending-completeness counts are printed as observability when scheduled
follow-up work is still active; they do not fail a representative proof once
the required aggregate evidence has landed.

In representative mode the package, advisory-evidence, impact-finding,
security-alert reconciliation, SBOM attachment, and container-image identity
counters default to minimum `1`. If a representative corpus explicitly sets one
of those minimums to `0`, the verifier skips that probe instead of turning an
unrequired read surface into a proof blocker. The advisory-evidence probe is
scoped by `ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID`; when unset, it falls back
to `ESHU_VULNERABILITY_E2E_CVE_ID`, then `CVE-2021-44228`. API probes are
bounded by `ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS`, which defaults to `30`.

Use these env vars only to make the recorded corpus contract more explicit:

```text
ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID=
ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS=30
ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT=100
ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT=
ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT=
ESHU_REMOTE_E2E_MIN_IMPACT_FINDING_COUNT=
ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT=
ESHU_REMOTE_E2E_MIN_SBOM_ATTACHMENT_COUNT=
ESHU_REMOTE_E2E_MIN_CONTAINER_IMAGE_IDENTITY_COUNT=
ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID=
ESHU_REMOTE_E2E_TARGET_STORY_FILE=
```

Set `ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID` only when the recorded
representative corpus intentionally includes an oversized package-registry
metadata document. The runtime verifier then proves the package's impact
readiness reports `target_kind=package_registry_metadata` with
`reason=metadata_too_large`, rather than accepting a retrying or terminally
failed collector claim as expected evidence.

The output is aggregate-only. Do not paste repository names, package names,
alert URLs, tokens, hostnames, or machine paths into public issues, docs, or PR
evidence.

The public-safe corpus coverage contract also requires a
`relationship_evidence` evidence-family count. Count it from durable
`relationship_evidence_facts` and `resolved_relationships` rows that have a
matching API and MCP relationship evidence readback. A representative corpus
that covers package/advisory aggregates but has no durable relationship
drilldown remains partial; use a public follow-up issue reference rather than
raising the corpus size or falling back to full-corpus mode for routine
correctness proof.

No-Regression Evidence: `scripts/test-remote-e2e-corpus-preflight.sh` and
`scripts/test-verify-remote-e2e-runtime-state.sh` cover representative corpus
bounds, unknown modes, strict terminal queue state, representative scoped
terminal state, failed/retrying/dead-letter guardrails, and aggregate counter
thresholds. The verifier harness also proves API tokens are not exposed in
curl process arguments, API calls carry a max-time, and representative
aggregate probes with explicit minimum `0` are skipped.

Observability Evidence: `scripts/verify_remote_e2e_runtime_state.sh` reports
strict terminal queue counts or representative scoped terminal counts including
`dead_letter`, workflow convergence, and pending completeness, plus aggregate
package, advisory-evidence, impact-finding, security-alert reconciliation,
SBOM attachment, and container-image identity counters. When configured with
`ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID`, it also reports the bounded
`package_registry_metadata_too_large_gaps` count from the readiness API so an
expected size-limit coverage gap is visible without exposing private package
names, registry URLs, or credentials.

No-Regression Evidence: `scripts/test-e2e-evidence-manifest.sh` covers the
public-safe E2E manifest contract for `collectors.sbom_document`,
`collectors.scanner_worker`, `reducers.sbom_attachment`, readback counters,
queue counters, observability capture, classified skipped/unsupported rows, and
privacy rejection. It proves stale `collectors.sbom_attestation` rows are
rejected so SBOM source facts, reducer attachment truth, and scanner-worker
source evidence cannot be conflated. The validator also rejects required
ecosystem and evidence-family rows that claim `pass` with `count=0`, while
allowing a `partial` manifest to classify missing corpus slots with
`unsupported`, `skipped`, or `fail` plus a reason and public follow-up issue.

No-Observability-Change: this manifest validator change only classifies
operator-submitted aggregate evidence. Runtime diagnosis still uses Docker
service health, `/api/v0/index-status`, workflow coordinator status, fact
source counts, proof-matrix ecosystem and evidence-family counts, queue
counters, scanner-worker metrics/spans/logs, pprof reachability, log capture,
and resource snapshot capture.
