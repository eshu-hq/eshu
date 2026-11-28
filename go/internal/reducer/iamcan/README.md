# iamcan

Projects AWS IAM permission facts into the two canonical "what can this
principal actually do" graph edges: `CAN_ASSUME` and `CAN_PERFORM`.

This package moved out of the flat `internal/reducer` root under issue #6061. It
is a domain family: it owns two handlers and the pipeline behind them, and
nothing else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `IAMCanAssumeMaterializationHandler` | `iam_can_assume_materialization.go` | the CAN_ASSUME handler and its readiness gate |
| CAN_ASSUME edge rows | `iam_can_assume_edge_rows.go` | resolves trust statements to (principal, role) edge rows |
| `IAMCanPerformMaterializationHandler` | `iam_can_perform_materialization.go` | the CAN_PERFORM handler and its multi-keyspace readiness gate |
| CAN_PERFORM extraction | `iam_can_perform.go` | folds identity policies into grants and grants into edge rows |
| `Action` / `CatalogByAction` | `iam_can_perform_catalog.go` | the closed, reviewed catalog of sensitive actions |
| grant builder | `iam_can_perform_grant.go` | the CAN_PERFORM-specific fold, tallying into the CAN_PERFORM catalog |
| target resolution | `iam_can_perform_target_resolution.go` | exact ARN -> single glob -> ambiguous -> unresolved |
| cross-scope targets | `perform_cross_scope.go` | `CrossScopeTargetLoader` port, per-ARN readiness classification (#6785) |
| cross-scope wait | `perform_readiness_wait.go` | commit-first wait over `crossscope.DecideWait`, cheap poll path, wait telemetry (#6785) |
| resource policies | `iam_can_perform_resource_policy.go` | the cross-principal grants a resource policy adds |
| permission boundaries | `iam_can_perform_boundary.go` | the intersection that removes boundary-blocked grants |
| skip tally | `iam_can_perform_tally.go` | the bounded skip-reason accounting behind the counters |

Both slices are deliberate under-approximations. An edge is emitted only when
the grant is unconditioned, un-denied, and the target resolves to exactly one
scanned CloudResource node of the expected type. Wildcards, ambiguity,
conditions, permission boundaries and unscanned targets all degrade to a counted
skip, never to a guessed edge. A CAN_PERFORM edge is a claim an operator can act
on; that only holds while the refusals stay conservative.

## Cross-scope targets (#6785)

The awscloud collector writes roles and `aws_iam_permission` into the IAM
service scope (`aws:<account>:<claim-region>:iam`) and every catalog target into
its own service scope (`aws:<account>:<region>:s3`, `...:kms`, and so on). A
same-scope join therefore resolves no production target. When
`CrossScopeTargets` is wired, the handler:

- collects the exact Allow identity-policy resource ARNs of a catalog type in
  the permission's own account (S3 bucket ARNs carry no account or region, so
  any region of that account);
- asks the loader for candidate-scope readiness and the matching
  `aws_resource` facts, pinned to each scope's active generation;
- resolves a target only from a scope whose CloudResource nodes committed, and
  indexes those facts separately so they satisfy exact-ARN matches only. Glob
  patterns stay local: a glob over a view that holds only exactly-named ARNs
  could report one match where the account has several;
- commits first: the scope-wide retract and rewrite runs with every resolved
  target, and missing targets are left out (unresolved);
- then waits with `iam_can_perform_target_not_ready` (non-counting) while a
  target sits in an uncommitted scope, could still land in a never-activated
  pending scope, or names a scope that is not registered at all yet (derived
  from the ARN: `aws:<account>:<region>:<service>`, any region for S3).
  Wildcard and glob patterns name no concrete scope and keep the local-only
  skip semantics.

The wait is keyed by `(scope_id, domain)` in `reducer_readiness_waits`
(`ReadinessWaits`, wired to `wait.Store` (`storage/postgres/readiness/wait`)), so its anchor survives a
superseding generation. An unchanged poll asks the loader only about the
missing ARNs and writes nothing. At first-defer plus `ReadinessMaxWait`
(default 30 minutes) the missing set settles (`abandoned`); later generations
with the same set commit at once (`settled_missing`). A revoked grant is
retracted at the first claim of every generation, even while a target is
missing.

The first-generation retract skip uses the shared
`reducercontract.DecideFirstGenerationRetract` predicate (#8167), the same one
the RUNS_IMAGE handler uses: only a first attempt with no strictly older
generation skips, and a newer generation never counts. A same-generation
re-commit always retracts (reason `recommit`). An evaluation whose wait
decision finds the commit already done runs no retract and logs
`skip_retract=true` with reason `already_committed`. The handler span and the
completion log carry `skip_retract` and `skip_retract_reason`, and the reason
is never empty on this handler.

A newer pending generation beside an active one does not defer. A resource
added in a later target generation waits for the next IAM generation; the
account-scoped re-enqueue is being built in a separate PR (see the design note).
Resource-policy grantee resolution stays same-scope. See
#6785.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/cloudjoin`, `reducer/crossscope`, `reducer/factdecode`, `reducer/factload`,
`reducer/gpphase`, `reducer/iampolicy`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/graph/edgetype`,
`internal/telemetry`, `internal/truth` and the factschema SDK. It never imports
the parent `internal/reducer` package. The dependency runs the other way: the
root keeps compatibility aliases in the iam-can stanza of `compat_projection.go` for the four wiring
types the reducer command and the cypher writers name
(`IAMCanAssumeEdgeWriter`, `IAMCanPerformEdgeWriter`, and the two handlers) plus
the three readiness failure classes `internal/storage/postgres` classifies queue
rows with.

Two shared leaves were carved out for this move, because their symbols have
consumers on both sides of the boundary and neither side may import the other:

- `reducer/cloudjoin` — the CloudResource join index, also used by the AWS
  relationship and security-group slices at the root and the IAM-escalation
  slice in `reducer/iamescalation`;
- `reducer/iampolicy` — the IAM statement/grant/target vocabulary, also used by
  the IAM privilege-escalation slice in `reducer/iamescalation`.

`gpphase.KeyFromScope` was extracted for the same reason: both handlers gate on
the canonical-nodes-committed readiness phase, and the key derivation used to
exist only at the root inside `graphProjectionPhaseStateForIntent`. There is one
such function, not two agreeing ones -- the root publisher and both handlers all
call `gpphase.KeyFromScope`, and it derives the acceptance unit through the
single `gpphase.AcceptanceUnitID` helper -- so the family and the publisher
cannot read different keys.

## Telemetry

| signal | where | dimensions |
|---|---|---|
| `eshu_dp_iam_can_assume_edges_total` | `iam_can_assume_materialization.go` | `principal_kind`, `resolution_mode` |
| `eshu_dp_iam_can_perform_edges_total` | `iam_can_perform_materialization.go` | `resolution_mode` |
| `eshu_dp_iam_can_perform_skipped_total` | `iam_can_perform_materialization.go` | `skip_reason` |
| `eshu_dp_iam_can_perform_conditioned_total` | `iam_can_perform_materialization.go` | `confidence` |
| `eshu_dp_iam_can_perform_cross_scope_targets_total` | `perform_cross_scope.go` | `outcome` (resolved/unresolved/not_ready/scope_unregistered/abandoned/glob_local_only) |
| `eshu_dp_reducer_readiness_waits_total` | `perform_cross_scope.go` | `domain`, `outcome` (deferred/abandoned) |

The skipped counter is the first place to look when edges stop appearing: every
conservative refusal increments it under a named reason rather than vanishing.
Facts rejected for a malformed payload increment the shared
`eshu_dp_reducer_input_invalid_facts_total` counter instead, and the reducer
executions that run these handlers stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: hoist it to a shared-core
  tier (`payloadcore` for generic helpers, `contract` for vocabulary,
  `iampolicy` for IAM statement semantics, `cloudjoin` for node identity) and
  leave a root alias, rather than reaching upward.
- **The action catalog is a security boundary, not a lookup table.** Adding an
  entry to `iamCanPerformCatalog` widens what the graph asserts about real
  permissions. It is a security-sensitive change with its own review, not a
  style nit, and an action outside it is counted `skipped_uncatalogued_action`
  rather than silently dropped.
- **Partial wildcards are not expanded on purpose.** `iam:Create*` grants
  nothing here. Expanding it would over-approximate the grant, and an
  over-approximated grant becomes an edge asserting access that may not exist.
- **A skip is not a failure.** The handler returns success with a populated skip
  tally when a grant cannot be resolved confidently; do not "fix" a rising
  `skipped_ambiguous` by loosening resolution.
- **`assumeEdgeKey` is function-local and unrelated to `iampolicy.EdgeKey`.** It
  dedupes CAN_ASSUME rows by (principal, role); the shared `EdgeKey` dedupes by
  (principal, target). Same shape, different identity.

## Related docs

- [Reducer package](../README.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
