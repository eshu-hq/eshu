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
--artifacts <run-dir>`). No separate capture exists for the diagnostics route.

## Coverage gap — diagnostics-specific fields are not independently captured

`inventory.json` records only the fields the shared verifier asserts:
`installed`, `enabled`, `trusted`, `manifest_digest`. The diagnostics response
additionally carries per-component fields that inventory's list rows also
technically populate but that no proof artifact captures or asserts from a
live run: `trust_decision` (decision/code/reason), `policy_gate`
(state/mode/code), `scheduler_state`, `read_model_availability`, and
`last_conformance_proof`. Those fields come from the same sanitized readback
row, so they carry the same deployed-truth guarantee as `installed` /
`enabled` / `trusted` do — but that is an inference from shared code, not a
committed artifact that shows their live values.

**This capability's production restore to `supported` rests on the shared
data-path argument above, not on a distinct captured diagnostics readback.**
A follow-up that extends `scripts/run-remote-e2e-component-extension.sh` to
also invoke `docker exec <collector> eshu component list --json` filtered to
the reference component (or curl the live `.../diagnostics` route) and assert
`trust_decision.decision`, `policy_gate.state`, and
`read_model_availability.state` in a `diagnostics.json` artifact would close
this gap with the same rigor `inventory.json` has today. Recommended as a
near-term hardening item rather than a blocker, since the shared-handler
argument is sound (`readbackOrUnavailable` is one function, not two
diverging code paths) and is already covered end-to-end by the Go handler
tests below.

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
