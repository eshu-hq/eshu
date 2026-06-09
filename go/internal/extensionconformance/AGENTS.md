# AGENTS.md - internal/extensionconformance guidance for LLM assistants

## Read first

1. `go/internal/extensionconformance/README.md` - package purpose, ownership
   boundary, and fixture-mode flow.
2. `go/internal/extensionconformance/conformance.go` - report contract and
   validation orchestration.
3. `go/internal/component/manifest.go` - component manifest contract.
4. `sdk/go/collector/validation.go` - collector SDK result validation.
5. `docs/public/reference/component-package-manager.md` - user-facing CLI
   behavior.

## Invariants this package enforces

- Conformance is read-only. It must not install packages, enable components,
  claim work, write facts, or mutate graph state.
- Manifest validation stays owned by `go/internal/component`.
- Fixture result validation stays owned by `sdk/go/collector`.
- Findings must classify publication and hosted-activation blockers
  independently so release tooling and operators can make the same decision.
- Compose mode must not imply remote runtime proof unless the harness actually
  ran that proof and captured evidence.

## Common changes and how to scope them

- **Add a finding code** -> update `conformance.go`, package tests, CLI JSON
  output tests, and the component package manager docs.
- **Add reducer/query truth checks** -> keep graph/API reads behind narrow
  interfaces consumed by this package, add bounded tests, and use the
  `eshu-correlation-truth` and `eshu-mcp-call-rigor` skills.
- **Add Compose-backed proof** -> use `eshu-diagnostic-rigor`, keep hostnames
  and private paths out of docs, and record exact proof commands in the PR.

## Anti-patterns specific to this package

- Do not duplicate component manifest validation rules here.
- Do not accept undeclared fact kinds or unknown source confidence as warnings.
- Do not turn missing reducer/query consumers into non-blocking notices.
- Do not write private fixture payloads, credentials, or host paths into docs.
