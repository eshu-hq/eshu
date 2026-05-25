# Node Lockfile Parser

## Purpose

`internal/parser/nodelockfile` owns parsing for the Node and TypeScript
package-manager lockfiles that fall outside the JSON parser surface:
`yarn.lock` (Yarn classic v1 and Yarn Berry v2+) and `pnpm-lock.yaml`
(pnpm v6+). It emits `content_entity`-shaped dependency rows in the same
`variables` bucket the parent engine reads from `internal/parser/json`,
so downstream collectors, projectors, and the supply-chain impact reducer
do not need to learn about the underlying lockfile flavor.

## Ownership boundary

This package owns Yarn classic, Yarn Berry, and pnpm lockfile decoding,
dependency-chain reconstruction across importer-to-package edges, scope
distinctions (`runtime`, `dev`, `optional`, `peer`), the package-manager
flavor label (`yarn`, `pnpm`), and the workspace/file/link/portal exclusion
that keeps local code out of remote-evidence rows. It does not own parser
dispatch (parent engine), npm `package.json` or `package-lock.json` parsing
(`internal/parser/json`), package-registry identity resolution
(`internal/packageidentity`), or consumption admission (`internal/reducer`).

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse(path, isDependency, options)` returns one parser payload for a
  yarn or pnpm lockfile. The lockfile flavor is selected from filename and
  on-disk contents so misnamed files do not silently produce wrong rows.

## Dependencies

This package imports `internal/parser/shared` for `Options`, `BasePayload`,
and `ReadSource`, and `gopkg.in/yaml.v3` for pnpm YAML decoding. It must not
import `internal/parser`, collector, storage, query, projector, or reducer
packages.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing and failures
remain owned by the collector snapshot path and parent engine callers.

## Row contract

Every emitted row carries:

- `name`: package identity exactly as the lockfile records it.
- `value`: the resolved exact version (no range).
- `package_manager`: always the canonical npm ecosystem (`"npm"`) so SQL
  filters in `storage/postgres/owned_package_targets.go` and the
  consumption reducer match yarn and pnpm evidence under the same npm
  identity as `package.json` rows.
- `package_manager_flavor`: the explicit package manager tool
  (`"npm"`, `"yarn"`, or `"pnpm"`) so operators and readiness reads
  can tell which lockfile produced each row.
- `lockfile`: `true`.
- `lockfile_format`: `"yarn-classic"`, `"yarn-berry"`, or `"pnpm"`.
- `section`: `"yarn.lock"` for yarn rows; pnpm rows use `"runtime"`,
  `"dev"`, `"optional"`, `"peer"`, or `"pnpm-package"` for transitive
  entries.
- `dependency_path`, `dependency_depth`, `direct_dependency`: importer-side
  chain evidence reconstructed from package-to-package dependency edges.
- `lockfile_resolution_protocol` and `lockfile_unsupported_feature` for
  Yarn Berry rows that resolve through non-npm protocols (`patch:`, etc.).

## Invariants

- Workspace, `file:`, `link:`, `portal:` entries MUST NOT be emitted as
  remote-package rows. The lockfile does not prove a registry identity
  for those entries, and treating them as remote would invent false
  positives in vulnerability impact.
- Malformed or empty lockfiles MUST set `lockfile_parse_state` and emit
  zero dependency rows so missing evidence stays visible to readiness.
- Row order MUST be deterministic so downstream fact dedupe and reducer
  ordering stay stable across reparses.

## Related docs

- `docs/public/reference/dependency-coverage.md`
- `docs/public/reference/security-intelligence.md`
- `go/internal/parser/json/dependency_coverage.go`
