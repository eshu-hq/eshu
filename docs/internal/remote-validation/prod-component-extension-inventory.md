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

docker compose -p ce-proof \
  -f docker-compose.yaml \
  -f docs/public/run-locally/docker-compose.component-extension.yaml \
  --profile component-extension-collector --profile workflow-coordinator \
  down -v
```

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

Compose reconciled to all-healthy in well under a minute. Both planned
`component-work:*` scorecard work items reached `completed`. The observed
`GET /api/v0/component-extensions`-equivalent readback (captured via the
identical underlying registry function, see caveat below):

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

## Coverage caveat — CLI readback path, not a live HTTP request

`scripts/run-remote-e2e-component-extension.sh` captures `inventory.json` by
running `docker exec <collector> eshu component list --json` inside the
running collector container. That CLI command calls
`component.NewRegistry(home).Readback(policy)` directly
(`go/cmd/eshu/component.go:226`) — the **same** readback function the HTTP
handler `ComponentExtensionsHandler.listComponentExtensions` calls
(`go/internal/query/component_extensions.go:249`) before sanitizing and
wrapping it in the JSON envelope. The proof therefore exercises the identical
underlying registry-readback code path with identical data, against a real
deployed stack — but it does not issue an actual `GET
/api/v0/component-extensions` network request against a running query API
service. No `eshu` API/query service was exercised over HTTP by this harness;
the `eshu`, `mcp-server` services in the stack were up and healthy but were
not queried. A follow-up that curls the live route (`curl
http://<api>:8080/api/v0/component-extensions`) against the same stack would
close that last gap and is recommended before treating the HTTP wire contract
itself (routing, auth middleware, JSON envelope) as remotely proven —
`TestOpenAPISpecIncludesComponentExtensionRoutes` and the scoped-token route
tests in `go/internal/query/component_extensions_test.go` cover that wiring
locally today.

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
