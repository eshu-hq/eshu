# Access MCP namespace

Groups MCP access-posture route selection. The `posture` leaf selects the
five secrets/IAM posture tools (`list_secrets_iam_identity_trust_chains`,
`list_secrets_iam_privilege_posture_observations`,
`list_secrets_iam_secret_access_paths`, `list_secrets_iam_posture_gaps`,
`count_secrets_iam_posture`), mapping decoded arguments to a
dependency-neutral internal request without executing it.

## Ownership boundary

The leaf owns family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapter, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. Query execution stays in
`internal/query`. This single-child parent supplies a stable namespace for
related siblings and mirrors the `collector/access/posture` and
`projector/access/posture` names so the pipeline reads consistently.

### Move record (#6627)

Rename-only `secretsiam` to `access/posture` move per the #6692 owner
decision. Every exported Go symbol is identical; only import paths change.
The `secrets_iam_posture` fact kind is a wire contract in the fact-kind
registry and is unchanged — only package paths change. Tool names,
request paths, body keys, and telemetry are identical.

No-Observability-Change: no stage added and no metric, span, or log name
changed; this namespace declares no function, holds no state, and performs
no I/O of its own.

## Related docs

- [MCP architecture](../README.md)
