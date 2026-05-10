# AGENTS.md — internal/component guidance for LLM assistants

## Read first

1. `go/internal/component/README.md` — package purpose, flow, and invariants
2. `go/internal/component/manifest.go` — component manifest contract and
   validation
3. `go/internal/component/policy.go` — local trust modes and revocation checks
4. `go/internal/component/registry.go` — installed registry and activation
   state
5. `go/cmd/eshu/component.go` — CLI entry points that call this package
6. `docs/docs/reference/component-package-manager.md` — user-facing behavior
7. `docs/docs/reference/plugin-trust-model.md` — trust model constraints

## Invariants this package enforces

- **Installed is inert** — installation records a verified manifest and digest
  only. It must not start collectors, claim work, alter core schema, or mutate
  evidence.

- **Activation is explicit** — a component can run only after an enable step
  creates activation state for a named collector instance.

- **Claiming is explicit** — enabled components are not claim-capable unless
  their activation says so.

- **Trust fails closed** — rejected, revoked, unsupported, or unavailable
  provenance checks must block installation.

- **Registry state is durable** — update `registry.json` atomically and keep
  manifest copies under the component home so offline verification remains
  inspectable.

## Common changes and how to scope them

- **Add a manifest field** → update `manifest.go`, add validation tests in
  `manifest_test.go`, update `docs/docs/reference/component-package-manager.md`,
  and keep backward compatibility explicit.

- **Add a trust backend** → extend `Policy.Verify` through a narrow seam, add
  fail-closed tests in `policy_test.go`, and update the plugin trust model docs.

- **Change registry layout** → add migration or compatibility handling before
  changing paths or JSON shape; update `registry_test.go` with old and new
  state cases.

- **Add runtime launch behavior** → keep launch code outside this package. This
  package should only expose install and activation state for the runtime to
  consume.

## Anti-patterns specific to this package

- Do not treat an installed package as enabled.
- Do not make strict trust mode pass without a real provenance verifier.
- Do not silently downgrade revocation or provenance failures to warnings.
- Do not let packages execute arbitrary code during install.
- Do not add core database DDL hooks to component manifests.
