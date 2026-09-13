# CI/CD run projector namespace

This documentation-only package groups run-scoped projector intent builders.
It exports no runtime API and owns no shared state or lifecycle behavior.

The `correlation` child selects `ci.run` or `ci.artifact` evidence and builds
the reducer intent. Root projector assembly owns dispatch and enqueue behavior;
the CI/CD run reducer owns correlation and durable writes.

## Related docs

- [CI/CD projector namespace](../README.md)
- [Run-correlation intent builder](correlation/README.md)
