# #10620 OCI-registry, package-registry, security-alerts satellite cutover: in-tree removal and activation truth note

## Scope

Lane HEAD deletes the three in-tree producers (`go/cmd/collector-oci-registry`,
`go/cmd/collector-package-registry`, `go/cmd/collector-security-alerts` and
their source trees under `go/internal/collector/`) after their satellite
cutovers (#8861 OCI registry, #8862 package registry, #8866 security alerts),
and moves the shared types their in-core importers need into core-owned
non-collector packages: `go/internal/oci/registry` (provider vocabulary,
repository identity, normalization, pure per-provider parse constructors),
`packageidentity` visibility (`VisibilityUnknown/Public/Private` with wire
spellings), and the unread-store read state (`FanoutCursor`,
`TargetReadState`, `UnreadRepository`). The three importers are repointed
without behavior change (coordinator OCI planner 1:1 constructor renames,
package ecosystem targets, unread cursor/store; the old
`PackageIdentity` alias and `NormalizePackageIdentity` delegation already
forwarded to the new homes). Dropped along with the binaries: Dockerfile and
local-install build lines, cmd README entries, env-registry entries, the
collector fault-table cases, the manifest fault rows (exempted with
satellite-owned reasons), the dead wrapcheck exclusions, and the docs rows
that named the in-tree commands or packages.

Kept by design: the coordinator claim-planning paths, the fact kinds and
their SDK structs, the query/MCP read paths, the `eshu_dp_*` instrument
registrations in `go/internal/telemetry/instruments.go` (telemetry-coverage
rows now point at the instrument home), `testdata/cassettes/{ociregistry,
packageregistry,securityalerts}`, and the remote-e2e service names (satellite
workloads behind them; service-name migration is satellite follow-up).

## Live activation truth (deployment of record, observed at this HEAD)

- No workload in any namespace runs the in-tree
  `eshu-collector-oci-registry`, `eshu-collector-package-registry`, or
  `eshu-collector-security-alerts` binaries (two cluster-wide command
  sweeps).
- All three scopes are served by component-extension satellite activations
  for the OCI-registry, package-registry, and security-alerts scopes,
  ready. Exactly one producer holds each scope.

## Regression proof at this HEAD

No-Regression Evidence: this change removes the in-tree producers, moves
identity/cursor types byte-identically (modulo package renames), and repoints
(not rewrites) every consumer. No runtime, queue, lease, Cypher, or reducer
path is altered: the deleted packages have no remaining importers
(`go build ./...` clean on the whole module; every test binary in the module
compiles), the coordinator planner mapping is 1:1 over all 7 providers, the
read path is preserved (coordinator planner, unread store, entrypoints, and
new type-home suites green with `-count=1`), and corpus coverage is preserved
at 455/455 satisfied with zero uncovered or unresolved (3 fault rows moved to
satellite-owned exemptions).

No-Observability-Change: no metric, span, log key, or status signal is
added, removed, or relabeled. The per-fetch instrument registrations stay in
`go/internal/telemetry/instruments.go` (no emitter since the cutovers; the
telemetry-coverage rows now point at the instrument home or the surviving
type-home packages instead of the deleted packages).

- `go build ./...` clean (whole module).
- `go vet` clean on all touched packages via the floor (test files included).
- Focused suites green: `internal/coordinator/oci/registry`,
  `internal/coordinator/registry/package`,
  `internal/storage/postgres/security/alerts/unread`,
  `internal/collector/entrypoints`, `internal/oci/registry`,
  `internal/packageidentity` (`-count=1`).
- `go test` + `-race` on all 15 floor-selected changed packages: green.
- `go run ./cmd/capability-inventory -mode verify`: no findings.
- `bash scripts/verify-fact-kind-registry.sh`: generated artifacts current.
- `bash scripts/verify-collector-entrypoints-generated.sh` + mirror: current.
- `bash scripts/verify-telemetry-coverage.sh` (base `origin/main`): agree,
  no new untracked stages.
- Corpus mirror suite (`test-verify-golden-corpus-gate.sh`): pass; replay
  vehicle repointed at `collector-cicd-run` with byte-matching requires.
- Replay coverage gate test: green; report 455/455, dashboard regenerated.
- `bash scripts/verify-moved-file-refs.sh --base origin/main`: 211 vacated
  go paths, no dangling references.
- `bash scripts/verify-doc-citations.sh`: 294 test, 341 fixture, 254 raw
  line citations tracked, clean.
- Docs build (`mkdocs build --strict`): clean.
- Trimmed SDK README proof commands run green with real tests executing.

Not run here: the live Docker corpus gate (needs backends; covered by the
post-push CI wave), the live Postgres story matrices (fixtures compile; the
non-live suites pass), and `make pre-pr` (deletion-only change; deferred
touched-surface gates run directly instead).
