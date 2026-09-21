# AWS Scanner Relationship Graph-Join Guard

## Purpose

`internal/collector/awscloud/internal/relguard` is test-support code that
mechanizes the AWS scanner graph-join contract. Across the AWS scanner fleet the
single dominant correctness bug class was a relationship fact whose `target_type`
was empty, was not a real resource family, or was keyed by a name when the target
scanner publishes its `resource_id` as an ARN — so the edge dangled and never
joined its target node. Those defects were caught only by hand in review
(issue #804). This package makes the contract a test.

## The contract

For every relationship a scanner emits:

1. `target_type` is non-empty.
2. `target_type` is a known resource family: a declared `awscloud.ResourceType*`
   constant value, or a documented entry in `KnownTargetTypeAllowlist`.
3. When `target_arn` is set the target is ARN-keyed, so the join key
   (`target_resource_id`, or `target_arn` when the id is blank) is ARN-shaped.

## Two layers, one source of truth

`KnownTargetTypes(awscloudDir)` is the single valid-target-type set both layers
check against. It is the union of:

- every string value assigned to an `awscloud.ResourceType*` constant
  (parsed from the awscloud source with `go/parser`, no type checking); and
- `KnownTargetTypeAllowlist`, the explicit, commented set of forward references
  (targets Eshu does not scan yet) and synthetic/non-AWS join anchors
  (`container_image`, `git_repository`, CloudWatch metrics, etc.). Adding an
  entry here is a deliberate, reviewed decision that documents why a target is
  intentionally not a scanned resource.

### Static layer (repo-level guard)

`EmittedTargetTypeLiterals(servicesDir)` AST-walks every scanner package and
resolves the target_type expressions it can determine statically: inline string
literals, identifiers bound to package or file-local string constants, and
`awscloud.ResourceType*` selectors (recorded as const-backed, since the compiler
already guarantees those). `Validate` / `ValidateEmitted` assert each resolved
literal is non-empty and known. The live guard test
(`TestLiveScannerTreeHasNoGraphJoinDefects`) runs this over the real tree, so a
new scanner shipping an empty or unknown literal target_type fails CI.

### Runtime layer (per-scanner)

`Check` and `AssertObservations` take concrete `RelationshipObservation` values
and apply the same contract plus the ARN-shape and join-mode checks. This covers
the ~140 target types in the fleet that a helper call or a struct-field read
produces, which the static layer cannot resolve. A scanner test adopts it in one
line:

```go
relguard.AssertObservations(t, observations...)
```

## What it does and does not catch

Catches:

- empty `target_type` (static and runtime);
- a `target_type` that is neither a declared constant nor an allowlist entry
  (static and runtime);
- a populated `target_arn` that is not ARN-shaped (runtime);
- an ARN-keyed target whose `target_resource_id` is a bare name (runtime).

Does not catch:

- a fully data-dependent target_type that no test feeds through the runtime
  layer (the static layer cannot resolve it; adopt `AssertObservations`);
- a name-vs-ARN mismatch on an edge that sets neither `target_arn` nor an
  ARN-shaped `target_resource_id`, which is the documented
  name-keyed-with-correlation pattern (for example CloudWatch composite child
  alarms).

## Why it is not tautological

The known set is derived from the awscloud constant source and the explicit
allowlist. It never reads `awsruntime.SupportedServiceKinds()` or any runtime
registry, so the guard checks a real property of the source rather than
restating it.

## Ownership boundary

This package owns only the guard derivation and assertion helpers. It does not
register scanners, configure the runtime, or know per-service behavior beyond
the documented allowlist.

## Telemetry

None. This is test-support code that runs only under `go test`.

## Related docs

- `../../README.md` for the awscloud fact and envelope contract.
- `../../awsruntime/internal/guardset/README.md` for the sibling derived guard
  (scanner registration) this package's design mirrors.
- `docs/public/guides/collector-authoring.md` for the scanner authoring flow.
