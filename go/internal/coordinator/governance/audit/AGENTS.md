# AGENTS.md - internal/coordinator/governance/audit guidance

## Read first

1. `go/internal/coordinator/governance/audit/README.md` for the ownership
   boundary and the hash-stability invariants.
2. `go/internal/coordinator/governance/audit/event.go` — all four exported
   names live in one file.
3. `go/internal/coordinator/governance_audit.go` for the root's
   collector-egress and extension-egress emitters, which own the event type,
   scope class, decision, and reason code.
4. `go/internal/coordinator/semantic/provider_worker.go` for the second
   emitter, which appends through its own `GovernanceAuditAppender`.
5. `go/internal/governanceaudit/audit.go` for the `Event` shape and enum
   normalization this package does not own.

## Invariants

- Do not import the coordinator root. The root imports this package, so any
  edge back is a cycle.
- Do not add an appender interface here. Each consumer declares the append
  surface it needs; that is what keeps this package free of both the root and
  `internal/governanceaudit`.
- Do not change `Hash`'s NUL separator, its `sha256:` prefix, or
  `CorrelationID`'s 16-character truncation without a migration decision.
  Stored rows are not rewritten, so a change splits one logical scope across
  the change boundary.
- Keep raw scope values, credentials, hosts, and URLs out of anything this
  package returns. Its whole purpose is that the caller can emit an identity
  without emitting the subject.
- Keep `AppendTimeout` short. It is the only thing bounding an inline audit
  append on the reconcile and claim paths.

## Common changes

- Adding an identity helper: it belongs here only if more than one emitter
  needs it. A helper with one caller belongs with that caller.
- Changing a hash input: the change lives in the emitter's argument list, not
  in `Hash`. Adding or reordering parts changes every resulting hash, so treat
  it as a data migration and say so in the PR.
- Adding a consumer: give it its own appender interface and its own event
  construction; import this package only for identity and the timeout.

## Failure modes

- A hash-input change that looks cosmetic silently partitions a scope's audit
  history in two, and nothing fails at build or test time.
- A separator that can occur inside a part lets two distinct scopes collide on
  one hash, which is an accuracy failure in an audit trail.
- A longer `AppendTimeout` converts a wedged audit store into a stalled
  reconcile loop rather than a failed append.

## Verification

Run the two emitters' tests, since this package's behavior is observable only
through them: `go test ./internal/coordinator ./internal/coordinator/semantic
-count=1`. Build and vet the whole module, because the coordinator root and
`cmd/workflow-coordinator` own the concrete wiring.
