# AGENTS.md — code projector namespace guidance

Read `README.md`, `doc.go`, and `../AGENTS.md` before changing this tree.

This directory is a documentation-only namespace. Keep runtime logic in leaf
packages and root assembly in `../scope_generation_intents.go`. Do not add
imports, declarations, initialization, shared mutable state, queue ownership,
or telemetry to this namespace.

Preserve the separation between projector intent creation, reducer
materialization, and query read ownership. Do not describe these packages as
independently extractable while they depend on repository-internal contracts.
