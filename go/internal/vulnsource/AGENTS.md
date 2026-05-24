# Agent Notes

`internal/vulnsource` contains shared value types for vulnerability source
freshness, checkpoints, retry state, and status projection. Keep this package
dependency-light so collectors, storage, query, and status can share the same
closed enums without import cycles.

Do not put network clients, database calls, workflow mutation, or reducer truth
in this package. Runtime packages classify observations; storage packages
persist them; query/status packages render them.
