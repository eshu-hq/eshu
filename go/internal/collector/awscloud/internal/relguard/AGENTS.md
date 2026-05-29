# AGENTS - awscloud/internal/relguard guidance

## Read First

1. `README.md` - the graph-join contract, the two layers, and what the guard
   does and does not catch.
2. `doc.go` - godoc contract for the exported helpers.
3. `../../envelope.go` - `NewRelationshipEnvelope`, the emission path the guard
   protects. Note it does NOT validate `target_type`; relguard does.
4. `../../awsruntime/internal/guardset/AGENTS.md` - the sibling derived-guard
   precedent this package mirrors.

## Invariants

- NEVER import the `awsruntime` registry or read
  `awsruntime.SupportedServiceKinds()`. The valid target-type set MUST be
  derived from the awscloud constant source plus `KnownTargetTypeAllowlist`.
  Deriving it from the runtime would make the guard tautological.
- Keep the static layer source-based (`go/parser`, no type checking). It must
  stay fast and free of a `golang.org/x/tools/go/packages` dependency.
- Keep the negative proofs in `relguard_test.go`:
  `TestValidateFlagsEmptyAndUnknown` (static) and
  `TestRuntimeCheckCatchesDataDependentDefects` (runtime). They are the proof the
  guard catches the #804 defect class; do not delete them.
- `KnownTargetTypeAllowlist` entries MUST carry a comment explaining why the
  target is deliberately not a scanned resource. An entry without a rationale is
  a defect waiting to be re-laundered as "intentional".

## Common Changes

- A new scanner with a literal or constant target_type that names a real
  resource family needs NO change here: the guard derives the value from the
  `awscloud.ResourceType*` constants automatically.
- A new scanner whose target_type is genuinely a forward reference (target not
  scanned yet) or a synthetic/non-AWS anchor: add a commented entry to
  `KnownTargetTypeAllowlist`, and prefer fixing the dangling target later.
- A new scanner with data-dependent target_type (helper or field read): wire
  `relguard.AssertObservations(t, observations...)` into its scanner test so the
  runtime layer covers it.

## What Not To Change Without Review

- Do not turn this into runtime (non-test) code. It is test-support only.
- Do not relax the ARN-keying check to silence a real name-vs-ARN dangling edge;
  fix the scanner to key by the format the target scanner publishes instead.
- Do not add an allowlist entry to silence a value that should match an existing
  declared constant (for example `aws_firehose_delivery_stream` when the target
  is `aws_kinesis_firehose_delivery_stream`). Fix the scanner.
