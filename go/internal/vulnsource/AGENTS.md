# Agent Notes

`internal/vulnsource` contains shared value types for vulnerability source
freshness, checkpoints, retry state, and status projection. Keep this package
dependency-light so collectors, storage, query, and status can share the same
closed enums without import cycles.

`packageidentity` is the only Eshu import. Do not import `workflow` here: it
pulls the reducer tree into every package that shares these enums. Scope grammar
that duplicates a coordinator constant keeps a pinning test instead
(`TestDerivedOSVVirtualSlotCountMatchesWorkflow`).

Do not put network clients, database calls, workflow mutation, or reducer truth
in this package. Runtime packages classify observations; storage packages
persist them; query/status packages render them.
