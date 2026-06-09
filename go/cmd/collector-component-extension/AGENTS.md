# AGENTS.md - collector-component-extension guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/internal/collector/extensionhost/README.md`
4. `go/internal/component/README.md`
5. `docs/public/extend/community-extension-authoring.md`
6. `docs/public/reference/component-package-manager.md`

## Invariants

- Keep extensions outside Eshu internals. Extension processes receive only the
  bounded SDK JSON request.
- Keep the raw activation config path out of workflow rows, logs, metrics, API
  responses, and MCP responses. Use the shared component config handle.
- Trust policy must fail closed before the worker can claim work.
- Do not add graph writes here. Extension facts are source evidence until a
  reducer owns a consumer contract.
- Do not bypass `collector.ClaimedService` for claims, retries, heartbeats,
  terminal failure, or durable commit.

## Common changes

- Add config env vars in `config.go`, docs in `README.md`, and Compose docs in
  `docs/public/run-locally/docker-compose.md`.
- Add a new adapter only with focused tests for config loading, runner
  construction, failure classes, and claim-safe process boundaries.
- Keep adapter commands shell-free: pass command and args directly to
  `exec.CommandContext` or an equivalent bounded runner.

## Failure modes

- Coordinator and worker point at different `ESHU_COMPONENT_HOME` values.
- Trust envs differ between coordinator and worker, causing planned work to
  exist while the worker refuses the activation.
- The process runner returns a result with mismatched claim identity; the host
  must terminal-fail the claim before commit.
- Multiple trusted activations exist and no instance ID is selected.

## Verification

Run focused checks before broader gates:

```bash
go test ./cmd/collector-component-extension -count=1
go test ./internal/collector/extensionhost ./internal/component -count=1
```

Runtime or Compose changes also require:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```
