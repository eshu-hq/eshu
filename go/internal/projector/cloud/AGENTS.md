# AGENTS.md — cloud projector namespace guidance

Read `README.md`, `doc.go`, and `../AGENTS.md` before changing this tree.

Keep this directory a documentation-only namespace. Runtime behavior belongs
in leaf packages, and root assembly belongs in
`../scope_generation_intents.go`. Do not add imports, declarations,
initialization, shared mutable state, queue ownership, or telemetry here.

Preserve the separation between inventory admission, runtime-drift intent
creation, reducer materialization, and query ownership. Do not describe these
packages as independently extractable while they depend on repository-internal
contracts.
