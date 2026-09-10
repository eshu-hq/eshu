# AGENTS.md — cloud runtime-drift projector namespace guidance

Read `README.md`, `doc.go`, `../AGENTS.md`, `../../AGENTS.md`, and
`../../../AGENTS.md` first.

Keep this namespace documentation-only and keep `aws/` and `multi/` as separate
leaves. Do not combine their triggers, domains, entity keys, or reducer
contracts. Root assembly owns invocation order, queue writes, retries, and
telemetry; the reducers own runtime-drift materialization.
