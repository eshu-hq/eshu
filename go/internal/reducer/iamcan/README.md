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
| resource policies | `iam_can_perform_resource_policy.go` | the cross-principal grants a resource policy adds |
| permission boundaries | `iam_can_perform_boundary.go` | the intersection that removes boundary-blocked grants |
| skip tally | `iam_can_perform_tally.go` | the bounded skip-reason accounting behind the counters |

Both slices are deliberate under-approximations. An edge is emitted only when
the grant is unconditioned, un-denied, and the target resolves to exactly one
scanned CloudResource node of the expected type. Wildcards, ambiguity,
conditions, permission boundaries and unscanned targets all degrade to a counted
skip, never to a guessed edge. A CAN_PERFORM edge is a claim an operator can act
on; that only holds while the refusals stay conservative.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/cloudjoin`, `reducer/factdecode`, `reducer/factload`,
`reducer/gpphase`, `reducer/iampolicy`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/graph/edgetype`,
`internal/telemetry`, `internal/truth` and the factschema SDK. It never imports
the parent `internal/reducer` package. The dependency runs the other way: the
root keeps compatibility aliases in `iam_can_compat.go` for the four wiring
types the reducer command and the cypher writers name
(`IAMCanAssumeEdgeWriter`, `IAMCanPerformEdgeWriter`, and the two handlers) plus
the two readiness failure classes `internal/storage/postgres` classifies queue
rows with.

Two shared leaves were carved out for this move, because their symbols have
consumers on both sides of the boundary and neither side may import the other:

- `reducer/cloudjoin` — the CloudResource join index, also used by the AWS
  relationship, security-group and IAM-escalation slices at the root;
- `reducer/iampolicy` — the IAM statement/grant/target vocabulary, also used by
  the IAM privilege-escalation slice at the root.

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

The skipped counter is the first place to look when edges stop appearing: every
conservative refusal increments it under a named reason rather than vanishing.
Facts rejected for a malformed payload increment the shared
`eshu_dp_reducer_input_invalid_facts_total` counter instead, and the reducer
executions that run these handlers stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk inside the ten moved production files is a package
clause, an import requalification, or an identifier requalification: symbols the
reducer root supplied as one-line forwarders are now imported from the leaf that
already owned them (`payloadcore` for the deref/tally/payload helpers,
`contract` for the intent, result, ownership and domain vocabulary,
`factload`/`factdecode`/`schemadecode`/`gpphase` for the rest), and symbols with
consumers on both sides moved to `cloudjoin` and `iampolicy` with their bodies
untouched. The one behavioral seam is the readiness gate: both handlers used to
build a full phase *state* and read `.Key` off it, and now call
`gpphase.KeyFromScope` directly. The key is byte-identical -- the state
constructor did nothing else to it, and the root's own constructor calls that
same function -- and the discarded half was two timestamps the gate never read.
A Go import change adds no indirection at runtime. Measured on this branch:
`go build ./...` exits 0, `go vet ./internal/reducer/...` exits 0, and `go test
./internal/reducer/... -count=1` passes, including this package. Binary output
was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. The four counters above and the executions that wrap them are the
same before and after the move.

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
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
