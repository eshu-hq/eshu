# internal/eshulocal

## Purpose

`internal/eshulocal` owns the local data-root contract used by `eshu graph
start`: workspace identity, on-disk layout, owner locking, owner records,
layout versioning, embedded Postgres startup, health checks, and safe reclaim.

## Ownership boundary

This package owns local filesystem and process coordination only. It does not
start NornicDB, run graph projection, parse repositories, or manage deployed
Postgres. Callers wire its layout and DSN helpers into local runtime commands.

## Exported surface

See `doc.go` for the contract. Main exported surfaces include `Layout`,
`ResolveLayout`, owner read/write helpers, `AcquireOwnerLock`,
`ErrWorkspaceOwned`, `EnsureLayoutVersion`, `StartEmbeddedPostgres`,
`ManagedPostgres`, `PostgresDSN`, reclaim helpers, and platform health probes.

Windows files intentionally return stubs for unsupported embedded-Postgres and
owner-lock behavior; Unix files implement the local runtime path.

## Dependencies

The package uses the standard library plus the local data-root specs. It is
consumed by local CLI/runtime code and should stay free of collector, parser,
reducer, query, and graph-backend dependencies.

## Telemetry

The package emits no metrics or spans directly. Local runtime callers own
operator-facing logs and status. Embedded Postgres stdout/stderr is routed to
the workspace `postgres.log` so foreground local runs stay readable.

## Gotchas / invariants

- The workspace ID and directory layout are compatibility contracts. Do not
  change them without updating the local data-root specs.
- Hold `owner.lock` before writing `owner.json` or reclaiming embedded
  Postgres state.
- Reclaim only stops an ownerless live `postmaster.pid` when PID, socket, and
  protocol checks agree.
- `PostgresDSN` and `LocalQueryProfile` are local-runtime helpers; deployed
  runtimes use normal environment configuration.
- Layout version mismatches should fail clearly instead of silently migrating
  data.

## Focused tests

```bash
cd go
go test ./internal/eshulocal -run 'Test.*Layout|Test.*Owner|Test.*Reclaim|Test.*Postgres|Test.*Version|Test.*Startup' -count=1
go test ./internal/eshulocal -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

## Related docs

- `docs/public/reference/local-data-root-spec.md`
- `docs/public/reference/local-host-lifecycle.md`
- `docs/public/reference/local-testing.md`
