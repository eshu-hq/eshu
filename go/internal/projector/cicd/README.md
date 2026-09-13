# CI/CD projector namespace

This documentation-only package groups projector intent builders by CI/CD
domain. It exports no runtime API and owns no mutable state, initialization,
queue behavior, storage, or telemetry.

The `run` child groups run-scoped projector families. Runtime assembly remains
in the root `internal/projector` package, while reducers own correlation and
durable writes.

## Related docs

- [Projector architecture](../README.md)
- [CI/CD run namespace](run/README.md)
