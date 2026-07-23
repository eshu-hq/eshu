# prod-component-extension-diagnostics — production validation

Capability: `component_extensions.diagnostics` (tool
`get_component_extension_diagnostics`, route `GET
/api/v0/component-extensions/{component_id}/diagnostics`).
Production profile: `required_runtime: deployed_services_component_registry`,
`max_scope_size: one_component`, `p95_latency_ms: 500`,
`max_truth_level: exact`.

## Claim validated

`getComponentExtensionDiagnostics` (`go/internal/query/component_extensions.go:185`)
shares its entire data path with `listComponentExtensions`: both call
`h.readbackOrUnavailable`, which calls the identical
`component.NewRegistry(h.ComponentHome).Readback(h.Policy)` used by inventory
and by the `eshu component list` CLI, then run the same
`sanitizedComponentExtensions` projection; diagnostics only additionally
filters that already-sanitized slice down to the requested `component_id`.
The deployed-stack proof recorded in
[prod-component-extension-inventory](prod-component-extension-inventory.md)
therefore exercises the identical registry-readback code path diagnostics
depends on, from the same run, against the same real installed/enabled/trusted
component.

## What was run (reproduction)

Same run as `prod-component-extension-inventory`: see that document's
reproduction steps
(`docker compose ... up`, `scripts/run-remote-e2e-component-extension.sh
--artifacts <run-dir>`, `scripts/verify-remote-e2e-component-extension.sh
--artifacts <run-dir>`), plus the same compose fix (`ESHU_COMPONENT_HOME` and
matching trust-policy env now set on the `eshu` and `mcp-server` services in
`docs/public/run-locally/docker-compose.component-extension.yaml`) and the
live HTTP capture below, run against the same reconciled stack.

## Live HTTP proof — GET /api/v0/component-extensions/{component_id}/diagnostics

```bash
token=$(docker exec ce2-eshu-1 sh -c \
  'grep "^ESHU_API_KEY=" /data/.eshu/.env | cut -d= -f2-')
curl -s -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer ${token}" \
  http://127.0.0.1:8080/api/v0/component-extensions/dev.eshu.examples.scorecard/diagnostics
# 200
```

Response (redacted — same sanitization as inventory; `config_handle` is an
opaque sha256 handle, never a filesystem path):

```json
{
  "schema_version": "eshu.component_extensions.v1",
  "status": "available",
  "component_home_configured": true,
  "component": {
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
  },
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

This closes the previous coverage gap: the diagnostics-specific fields
(`trust_decision`, `policy_gate`, `scheduler_state`,
`read_model_availability`, `last_conformance_proof`) are now captured live
from a real `GET .../diagnostics` network request against the deployed,
auth-gated query API service — `trust_decision.decision: "allowed"`,
`policy_gate.state: "allowed"`, and
`read_model_availability.state: "unavailable"` (reason
`missing_conformance_proof`, expected since the Scorecard reference component
does not publish a conformance proof) are all observed values, not an
inference from the shared-handler argument alone. The shared-handler argument
(`readbackOrUnavailable` is one function, not two diverging code paths) still
holds and is corroborated by this being the same route family, same auth
middleware, and same sanitization as inventory.

## Committed reproducible evidence

**Handler contract and sanitization (both list and diagnostics share this
function)** — `go/internal/query/component_extensions_test.go`:
`TestComponentExtensionsHandlerListsSanitizedInventoryAndDiagnostics`,
`TestComponentExtensionsHandlerReturnsUnavailableWhenComponentHomeUnset`,
`TestAuthMiddlewareWithScopedTokensAllowsComponentExtensionRoutes`,
`TestOpenAPISpecIncludesComponentExtensionRoutes`. Reproduce:

```bash
cd go && go test ./internal/query -run ComponentExtensions -count=1
```

**Deployed-stack shared-readback proof** — see
[prod-component-extension-inventory](prod-component-extension-inventory.md)
for the full reproduction, observed readback, and provenance.

## Notes

No private data: cited tests and the shared deployed-stack capture expose
only booleans, counts, reason codes, and version/digest strings.

Related: #5681 (this validation), #5682 (prior review that found the OCI
harness did not exercise the read surface), #5666 (downgrade this restores),
#5407 (artifact-existence gate), #5552 (burn-down).
