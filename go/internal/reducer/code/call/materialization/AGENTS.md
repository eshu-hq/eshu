# Agent instructions: internal/reducer/code/call/materialization

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Reduces one parser relationship follow-up into durable shared-intent
emission for code-call, Python metaclass, and symbol→runtime
(`HANDLES_ROUTE`/`RUNS_IN`/`INVOKES_CLOUD_ACTION`) rows (issue #6061). Moved
out of the reducer root as its own package. See the README's Purpose and
Ownership boundary sections for what this package owns and reuses from
`codecall`/`code/call/shared`.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/call/materialization/README.md`
- `go/internal/reducer/code/call/README.md` (the `codecall` sibling this package extracts through)
- `docs/internal/design/reducer-target-tree.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the code-call stanza of
  `compat_projection.go` and `defaults_domain_catalog.go`'s wiring), never
  the reverse.
- **Every symbol→runtime domain's per-edge rows MUST be paired with a
  repo-wide refresh row in the same `BuildIntentRows` pass.** Splitting them
  across passes breaks the worker's refresh-fence pairing invariant
  (#2898/#2910) — see `buildRepoWideRetractRefreshIntents`'s doc comment.
- **The resolved AWS action goes under payload key `"cloud_action"`, never
  `"action"`.** The shared-projection upsert filter reads `payload["action"]`
  as the write discriminator.

## Common changes

Adding a new AWS SDK (service, method) -> action mapping: add a row to
`cloudActionByServiceMethod` in `cloud_actions.go`, confirming the target
action's SDK v2 Go method name is a single unambiguous operation (see that
map's doc comment for the `s3:listbucket` counter-example) and that it is
present in the closed `iamcan` `CAN_PERFORM` catalog.

## Failure modes to avoid

- Adding a local copy of `codecall`'s extraction/entity-index/file-scope
  helpers instead of importing `codecall`/`code/call/shared` directly — this
  package already sits below them in the dependency graph.
- Exporting a new unexported helper "just in case" a future caller needs it.
  `Handler`, `IntentWriter`, `CanonicalNodeChecker`, `BuildIntentRows`,
  `ExtractIntentRows`, and `BuildRouteIntentRowsForQueryProof` are exported
  because the reducer root's compat forwarders and external callers
  (internal/ifa, internal/mcp, internal/query) need them; nothing else in
  this package has an external caller today.

## Do not change without ADR review

- The evidence-source string constants (`handlesRouteEvidenceSource`,
  `runsInEvidenceSource`, `invokesCloudActionEvidenceSource` in
  `routes.go`/`workloads.go`/`cloud_actions.go`) and the `cloud-action:`
  node-id prefix (`cloudActionIDPrefix`) — durable rows and downstream
  consumers key on these literals.
