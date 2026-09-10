# scheduled — scoped agent instructions

## Read first

1. `./README.md` for what this package owns and its split from
   `freshness`.
2. `../../../AGENTS.md` for coordinator-wide invariants, including the recorded
   decision that AWS target-scope parsing has a single definition.
3. `../freshness/README.md` — the sibling this package calls into.

## Invariants

- Call `freshness.ParseTargetScopes`; never copy the shared configuration
  parser. Keep `awsScheduledTargetAllowed` local because scheduled global and
  regional service pairing is distinct from freshness-trigger authorization.
- Keep the local `scheduled_scan_enabled` decode (`ScanEnabled`) here. It is the
  sibling flag to target-scope parsing, and root calls it rather than keeping
  its own copy.
- Do not import the root `coordinator` package; root imports this one to wire
  the `AWSScheduledPlanner` port, and the reverse import cycles.
- `aws_bindings_test.go` must stay. It is the only thing populating the AWS
  scanner registry for this package's test binary.

## Anti-patterns

- Do not widen the export surface past `PlanRequest`, `WorkPlanner`, and
  `ScanEnabled`.
- Do not move the `Service.Run` cases here; they need root's `fakeStore`.
- Do not "deduplicate" `ScanEnabled` into `freshness`; the two flags
  are decoded by the family that owns them.
