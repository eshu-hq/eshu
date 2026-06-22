# Hosted Redaction Registry

Hosted governance uses `go/internal/redact.HostedGovernanceRegistry` as the
central redaction matrix for optional provider, collector, extension, API, MCP,
console, audit, status, docs, and onboarding surfaces. Source-specific packages
still own their provider schemas and sensitive-key lists; the registry defines
the shared negative-leakage contract those packages must satisfy.

## Surface Matrix

| Surface | Forbidden raw classes | Safe bounded classes |
| --- | --- | --- |
| Facts | raw tokens, provider keys, prompts/provider payloads, private URLs, secret values, direct personal identifiers, private source identifiers | credential source kind, credential reference, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Logs | same forbidden classes | credential source kind, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Metric labels | same forbidden classes | credential source kind, source class, actor class, reason code, policy state, collector kind |
| Status errors | same forbidden classes | credential source kind, credential reference, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Graph properties | same forbidden classes | credential source kind, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| API/MCP bodies | same forbidden classes | credential source kind, credential reference, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Console surfaces | same forbidden classes | credential source kind, credential reference, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Audit events | same forbidden classes | credential source kind, credential reference, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Docs examples | same forbidden classes | credential source kind, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |
| Onboarding artifacts | same forbidden classes | credential source kind, credential reference, provider profile id, source class, tenant/workspace id, actor class, reason code, policy state, redaction marker, collector kind |

Metric labels intentionally exclude credential references, provider profile ids,
tenant/workspace ids, and redaction markers because those values are too easy to
turn into high-cardinality labels. Put them in bounded status, logs, audit
events, or response bodies only when the owning policy allows that surface.

## Canary Proof

The registry ships synthetic public canaries for these sensitive classes:

- raw token
- provider key
- prompt or provider payload
- private URL
- secret value
- direct personal identifier
- private source identifier

Use `Registry.AssertNoForbiddenCanary(surface, payload)` in focused tests after
the owning surface has already applied its source-specific redaction rules. The
helper returns the surface and sensitive class that leaked, but never echoes the
raw canary or payload.

For operator proof bundles, use the hosted governance negative-leakage verifier:

```bash
scripts/verify-hosted-governance-negative-leakage-proof.sh \
  --manifest leakage-proof.json \
  --output-json leakage-proof.summary.json \
  --output-markdown leakage-proof.summary.md
```

The manifest must cover facts, logs, metric labels, status errors, graph
properties, API bodies, MCP bodies, console surfaces, audit events, generated
docs, and onboarding artifacts. Referenced files stay local to the operator
proof environment; the summary records only counts and SHA-256 digests.

Registry canaries are not production secrets and are not a replacement for
source-specific fixtures. Cloud, Terraform, semantic, plugin, API, MCP, and
collector packages must keep their own schema-aware redaction tests because only
they know which provider fields are safe, redacted, or dropped.

## Adding A Surface Or Rule

1. Add the surface, sensitive class, or safe class in `go/internal/redact/registry.go`.
2. Add or update `go/internal/redact/registry_test.go` so missing surfaces,
   unsafe metric labels, duplicate canaries, and raw canary leakage fail.
3. Update this matrix in the same PR.
4. Keep raw credentials, provider URLs, private source identifiers, and real
   personal identifiers out of tests and docs.

No-Regression Evidence: `go test ./internal/redact -run 'TestHostedGovernanceRegistry' -count=1` proves the registry covers all hosted surfaces, detects forbidden synthetic canaries, keeps error text free of raw leaked values, permits sanitized payloads, and rejects high-cardinality metric-label classes.

No-Observability-Change: the registry is a pure in-memory contract. It emits no
metrics, spans, logs, status rows, facts, graph writes, API responses, MCP
payloads, audit events, or docs artifacts by itself.
