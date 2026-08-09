# prod-component-extension-diagnostics — production validation

Validation-Slug: prod-component-extension-diagnostics
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/run-remote-e2e-component-extension.sh
Validation-Command: CE_PROOF_PROJECT=eshu-5552-component-20260809-3 bash scripts/run-remote-e2e-component-extension.sh --artifacts /tmp/eshu-5552-component-artifacts-20260809-3; echo $?
Validation-Exit-Code: 0
Capability-Assertion: component_extensions.diagnostics returned allowed policy and trust decisions plus claim-capable scheduler state from the deployed component registry.
B12-Assertion: component_extensions.diagnostics -> mcp:get_component_extension_diagnostics

## Fresh deployed validation

The uniquely named Compose stack used an image rebuilt from commit
`bab7e38e6f`. The capture-and-verify driver proved the shared registry was
installed, enabled, trusted, terminal-successful, and fact-producing. Its
authenticated HTTP and MCP diagnostics calls both returned `available`, an
`allowed` trust decision, an `allowed` policy gate, and a `claim_capable`
scheduler state for the Scorecard component.

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
live HTTP/MCP captures below, run against the same reconciled stack.

## Live HTTP and MCP proof

```bash
CE_PROOF_PROJECT=eshu-5552-component-20260809-3 \
  bash scripts/run-remote-e2e-component-extension.sh \
  --artifacts /tmp/eshu-5552-component-artifacts-20260809-3
# component-extension proof artifacts verified (...)
# exit 0
```

The driver called both `GET
/api/v0/component-extensions/dev.eshu.examples.scorecard/diagnostics` and the
MCP tool `get_component_extension_diagnostics` with that exact component ID,
then verified each Eshu truth envelope independently.

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
    "installed_at": "2026-08-09T18:26:34.327080858Z",
    "states": ["installed", "enabled", "claim_capable"],
    "activations": [
      {
        "instance_id": "scorecard-remote",
        "mode": "scheduled",
        "claims_enabled": true,
        "config_handle": "component-config:5bc505367c526ee8d5ba4da5ff59c0f0910569a6a60102bbe04a446418a2ba12",
        "enabled_at": "2026-08-09T18:26:34.334141371Z"
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

This closes the previous coverage gap: both deployed surfaces captured the
diagnostics-specific fields (`trust_decision`, `policy_gate`,
`scheduler_state`, `read_model_availability`, `last_conformance_proof`) live
from the real diagnostics route against the auth-gated query services —
`trust_decision.decision: "allowed"`,
`policy_gate.state: "allowed"`, and
`read_model_availability.state: "unavailable"` (reason
`missing_conformance_proof`, expected since the Scorecard reference component
does not publish a conformance proof) are all observed values, not an
inference from the shared-handler argument alone. The shared-handler argument
(`readbackOrUnavailable` is one function, not two diverging code paths) still
holds and is corroborated by this being the same route family, same auth
middleware, and same sanitization as inventory.

The general B-12 golden stack intentionally has no component registry and
continues to prove the fail-closed `registry unavailable` posture. That refusal
is not used as production capability evidence; this dedicated deployed driver
and its positive HTTP/MCP captures are the matching-tier proof.

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
