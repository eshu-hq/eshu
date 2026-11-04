# AGENTS.md - collector-cicd-run command guidance

## Scope

This command wires the hosted GitHub Actions CI/CD run collector binary. It
loads workflow collector instance JSON, resolves `token_env`, `host_token_file`,
or `github_app_*_env` credential references, builds `ghactionsruntime.ClaimedSource`, and commits through
`collector.ClaimedService`.

## Rules

- Do not log, expose, or persist credential values.
- Keep repository names, run IDs, artifact names, and provider URLs out of
  metric labels.
- The command must use workflow claims and claim fencing; do not write directly
  to Postgres.
- Configuration must keep credential references (`token_env`, `host_token_file`,
  or `github_app_*_env`) separate from resolved runtime tokens and private keys.
- Add focused config tests for new environment variables or target shapes.
- `providerRoutedSource` must forward every optional hook `ClaimedService` finds
  by type assertion on its `Source`. A hook it hides stops working in this binary
  with no error (#10063). Prove a hook through the router, not only on its source.

## Claim Store

- Build `collector.ClaimedService.ControlStore` with `store.New`, never
  with `postgres.NewWorkflowControlStore` directly. `store.New` owns the
  `ESHU_WORKFLOW_CLAIM_PLANE_URL` selection, its validation, and its telemetry
  (#8875).
