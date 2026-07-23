# prod-component-extension-inventory — production validation

Capability: `component_extensions.inventory` (tool `list_component_extensions`,
route `GET /api/v0/component-extensions`).
Production profile: `required_runtime: deployed_services_component_registry`,
`max_scope_size: bounded_deployed_component_registry_page_1_500`,
`p95_latency_ms: 500`, `max_truth_level: exact`.

## Claim validated

A deployed component-extension stack — workflow coordinator in
active/claims-enabled mode, a component-extension collector, Postgres, and
NornicDB — installs, trusts, and enables a real out-of-tree component package
(the `dev.eshu.examples.scorecard` reference), plans and completes a claim for
it, and the component registry readback that backs both `eshu component list`
and the `GET /api/v0/component-extensions` HTTP handler (both call the
identical `component.Registry.Readback` function; see
`go/internal/query/component_extensions.go:249` and
`go/cmd/eshu/component.go:226`) reports `installed=true`, `enabled=true`, and
`trusted=true` for it from real deployed runtime state, not a fixture or a
mocked registry.

## What was run (reproduction)

```bash
docker build -t eshu:local -f Dockerfile .
docker compose -p ce-proof \
  -f docker-compose.yaml \
  -f docs/public/run-locally/docker-compose.component-extension.yaml \
  --profile component-extension-collector --profile workflow-coordinator \
  up -d --build

scripts/run-remote-e2e-component-extension.sh \
  --artifacts <run-dir>   # capture + verify in one step
scripts/verify-remote-e2e-component-extension.sh --artifacts <run-dir>

# Live HTTP readback against the public eshu (API) service — see
# "Live HTTP proof" below.
token=$(docker exec <eshu-container> sh -c \
  'grep "^ESHU_API_KEY=" /data/.eshu/.env | cut -d= -f2-')
curl -s -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:8080/api/v0/component-extensions
curl -s -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:8080/api/v0/component-extensions/dev.eshu.examples.scorecard/diagnostics

docker compose -p ce-proof \
  -f docker-compose.yaml \
  -f docs/public/run-locally/docker-compose.component-extension.yaml \
  --profile component-extension-collector --profile workflow-coordinator \
  down -v
```

**Compose fix required (#5688 review, this validation)**: the overlay
(`docs/public/run-locally/docker-compose.component-extension.yaml`) previously
set `ESHU_COMPONENT_HOME` only on `component-extension-install`,
`workflow-coordinator`, and `component-extension-collector`. The public
`eshu` (API) and `mcp-server` services already mount the same shared
`eshu_data` volume at `/data` (their `ESHU_HOME` is `/data/.eshu`) but never
received `ESHU_COMPONENT_HOME`, so `readbackOrUnavailable`
(`go/internal/query/component_extensions.go:236`) always fail-closed with
`503 component extension registry is unavailable` for both
`GET /api/v0/component-extensions` and its diagnostics route — the CLI
readback worked, but the deployed read route never did. The overlay now also
sets `ESHU_COMPONENT_HOME: /data/.eshu/components` plus the same
`ESHU_COMPONENT_TRUST_MODE` / `ESHU_COMPONENT_ALLOW_IDS` /
`ESHU_COMPONENT_ALLOW_PUBLISHERS` / `ESHU_COMPONENT_CORE_VERSION` trust-policy
values already used by `workflow-coordinator` on the `eshu` and `mcp-server`
services, and depends on `component-extension-install` so the registry is
populated before either starts. No new volume mount was needed — only the
missing environment. This mirrors a real deployment: the query services need
the same component-home mount and trust policy as the coordinator/collector to
serve accurate registry truth.

**Doc-recipe gap found and worked around**: the published recipe in
[Reference Scorecard Extension](../../public/extend/reference-scorecard-extension.md)
passes only `--profile component-extension-collector` to `docker compose up`.
`workflow-coordinator` carries its own `profiles: [workflow-coordinator]` tag
in the base `docker-compose.yaml` and is not a dependency of any service in
the `component-extension-collector` profile, so with only the documented flag
`docker compose ... config --services` does not include
`workflow-coordinator` and the coordinator that plans the Scorecard claim
never starts. Passing `--profile workflow-coordinator` in addition to
`--profile component-extension-collector` (as shown above) is required for
the stack to actually produce a completed claim; without it,
`workflow_work_items` stays empty and the verifier's workflow check fails
closed with "no completed/succeeded component workflow item". This is a
documentation/script defect worth a follow-up fix to
`docs/public/extend/reference-scorecard-extension.md` and
`docs/public/run-locally/docker-compose.component-extension.yaml`, tracked
separately from this validation.

## Observed (redacted)

Compose reconciled to all-healthy in well under a minute. The planned
`component-work:*` scorecard work item reached `completed`. The CLI-captured
readback (`scripts/run-remote-e2e-component-extension.sh`'s `inventory.json`)
and the live `GET /api/v0/component-extensions` HTTP readback (below) agree:

```json
{
  "component_id": "dev.eshu.examples.scorecard",
  "installed": true,
  "enabled": true,
  "trusted": true
}
```

Committed fact families (counts only): `dev.eshu.examples.scorecard.snapshot`
(1), `dev.eshu.examples.scorecard.check` (2),
`dev.eshu.examples.scorecard.warning` (1) — all non-zero, matching the
manifest's `source_evidence_only:no_graph_truth` reducer contract (no graph
nodes/edges asserted).

Provenance recorded: `eshu_commit` resolved (not `unknown`), `component_digest`
a well-formed `sha256:` value, `core_version=dev`, `backend=nornicdb`,
`queue_terminal_state=completed`, `metrics_handle=:9464/metrics`. No host
path, private-key marker, bearer token, or raw IP appeared in any artifact
(the verifier's redaction canary passed).

## Live HTTP proof — GET /api/v0/component-extensions

Against the same reconciled stack (compose fix above applied), a real
`Authorization: Bearer <token>` request to the public `eshu` API service's
mapped port returned `200` with the live sanitized registry readback — not
the pre-fix `503`:

```bash
token=$(docker exec ce2-eshu-1 sh -c \
  'grep "^ESHU_API_KEY=" /data/.eshu/.env | cut -d= -f2-')
curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:8080/api/v0/component-extensions
# 200
```

Response (redacted — `config_handle` is an opaque sha256 handle, never a
filesystem path; no host path, private key, bearer token, or raw IP appears):

```json
{
  "schema_version": "eshu.component_extensions.v1",
  "status": "available",
  "component_home_configured": true,
  "components": [
    {
      "id": "dev.eshu.examples.scorecard",
      "name": "Reference Scorecard collector",
      "publisher": "eshu-hq",
      "version": "0.1.0",
      "manifest_digest": "sha256:85aedc15bdf428a664a78dea55b9dae11ccf59bb92cca590ebacec5aab379698",
      "verified": true,
      "trust_mode": "allowlist",
      "installed_at": "2026-07-23T02:51:56.655606775Z",
      "states": ["installed", "enabled", "claim_capable"],
      "activations": [
        {
          "instance_id": "scorecard-remote",
          "mode": "scheduled",
          "claims_enabled": true,
          "config_handle": "component-config:5bc505367c526ee8d5ba4da5ff59c0f0910569a6a60102bbe04a446418a2ba12",
          "enabled_at": "2026-07-23T02:51:56.661950722Z"
        }
      ],
      "diagnostics": {"policy_configured": true, "policy_allowed": true, "policy_mode": "allowlist"},
      "trust_decision": {"decision": "allowed"},
      "policy_gate": {"state": "allowed", "mode": "allowlist"},
      "last_conformance_proof": {"status": "missing", "reason": "missing_conformance_proof"},
      "scheduler_state": {"state": "claim_capable", "reason": "activation_allows_claims"},
      "read_model_availability": {"state": "unavailable", "unavailable_reason": "missing_conformance_proof"}
    }
  ],
  "count": 1,
  "total_count": 1,
  "limit": 100,
  "truncated": false,
  "policy": {
    "mode": "allowlist",
    "allow_ids_configured": true,
    "allow_publishers_configured": true,
    "revoke_ids_configured": false,
    "revoke_publishers_configured": false,
    "core_version_configured": true
  }
}
```

This is the actual `GET /api/v0/component-extensions` network request against
a running, auth-gated query API service, over the `eshu` container's mapped
host port — not the CLI's direct in-process registry call. `installed`,
`enabled`, and `trusted` all resolve `true` from the live route
(`states` includes `installed`/`enabled`/`claim_capable`,
`verified: true`, `trust_decision.decision: "allowed"`), matching the CLI
readback and the deployed-truth claim above.
`TestOpenAPISpecIncludesComponentExtensionRoutes` and the scoped-token route
tests in `go/internal/query/component_extensions_test.go` cover the same
wiring locally; this is the deployed-network confirmation of it.

## Committed reproducible evidence

**Handler contract and sanitization** —
`go/internal/query/component_extensions_test.go`:
`TestComponentExtensionsHandlerListsSanitizedInventoryAndDiagnostics`,
`TestComponentExtensionsHandlerBoundsInventoryWithLimit`,
`TestComponentExtensionsHandlerReturnsUnavailableWhenComponentHomeUnset`,
`TestAuthMiddlewareWithScopedTokensAllowsComponentExtensionRoutes`,
`TestOpenAPISpecIncludesComponentExtensionRoutes`. Reproduce:

```bash
cd go && go test ./internal/query -run ComponentExtensions -count=1
```

**Deployed-stack claim/commit/readback proof** — this document, backed by
`scripts/run-remote-e2e-component-extension.sh` (capture) and
`scripts/verify-remote-e2e-component-extension.sh` (verify). Self-test the
verifier without a stack:

```bash
scripts/test-verify-remote-e2e-component-extension.sh
```

## Notes

No private data: artifacts hold booleans, counts, and version/digest strings
only, checked by the verifier's redaction canary; the reference component's
fact families are source evidence only, and no graph nodes or edges were
written for them.

Related: #5681 (this validation), #5682 (prior review that found the OCI
harness did not exercise the read surface), #5666 (downgrade this restores),
#5407 (artifact-existence gate), #5552 (burn-down).
