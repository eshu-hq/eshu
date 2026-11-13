<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# G8 removal-bound rollback rehearsal design (#9669)

Refs #9669, part of #6707. Status: design only — rehearsed nowhere, executed
nowhere. This file authorizes no cluster contact, no traffic change, and no
irreversible act. The live rehearsal runs once, at the frozen removal base,
after G2 closes and before G9. Anything that moves the base, scope,
deletion set, or pins voids this design's readiness and every later gate.

## Source ruling

Arbiter-eshu ruling on #9669 (2026-11-13, coordinator-posted gate ruling):

- G8 is the removal-bound rollback rehearsal (Shape-3 revert-removal): prove
  rollback AFTER the in-tree deletion, bound to the removal base and head.
- D12 pin-reversal and edge rehearsal #474 do NOT satisfy G8.
- Acceptance: executed steps with named holder, last-good digests, ArgoCD
  Synced+Healthy, bounded re-verification (`/health`, one authed read, MCP
  `tools/list`), state untouched, elapsed time recorded.
- Waiver path only: explicit owner Plan B waiver quoted on #9669 plus a filed
  revival issue; valid only with no irreversible state or schema change in
  the removal PR.
- Binding order: 9669-G4 (routes) → G7 refresh → final G2 → G8 → G9 →
  removal PR opens. G8 design may be built now; G8 closes only at the base.

## What Shape-3 means here

The removal PR deletes the in-tree relay/compat surface (the D13 deletion
set, frozen at the removal base). Shape-3 reverts that removal commit on a
rehearsal branch — restoring the prior core artifact containing
`go/cmd/api` — rebuilds and re-serves it, then proves the restore serves the
same bounded surface it served before removal. It is a revert of the removal,
not a pin flip (D12) and not an edge re-point (#474).

Shapes 1 (edge-restore) and 2 (satellite-to-satellite) in
`docs/internal/9669-flip-prep/paired-rollback-draft.md` stay valid for their
own triggers and do not count toward G8.

## Preconditions (all checked on rehearsal day, none assumed from today)

1. Removal base frozen: exact core SHA plus satellite SHAs and generator
   recorded by G2. Any change after the freeze voids G8 readiness.
2. Last-good coordinates captured: `repo:tag@digest` for the served core
   artifact containing `go/cmd/api`, from the pin record at the base.
3. ArgoCD app Synced+Healthy at the base before the rehearsal starts.
4. Named holder recorded on #9669 (rehearsal owner, not the design author).
5. No irreversible state or schema change inside the removal PR; otherwise
   only the waiver path (below) can stand in for execution.

## Rehearsal steps (executed once, at the base)

Each step ends on the check named beside it. Steps 1–2 and 6 run the stub
scripts in `scripts/9669-g8-*.sh` (plan output today, live flags on the day).
Record wall-clock time per step; the packet reports total elapsed time.

1. Freeze and record. Write the base pair, deletion-set SHA, and last-good
   digests into the rehearsal log. Check: log cites exact SHAs and digests.
2. Reverse the removal. On a rehearsal branch, `git revert` the removal
   commit (no re-resolution; a conflict stops the rehearsal and voids the
   run). Open the reverse PR through the normal review path. Check: revert
   applies cleanly and the PR diff equals the removal diff inverted.
3. Restore and re-serve. Build and serve the restored core artifact through
   the standard release path; re-pin the rehearsal target to the last-good
   coordinates. Check: deployment image digests equal the recorded last-good
   digests (`scripts/9669-g8-capture-digests.sh`).
4. ArgoCD green. The rehearsal app reads Synced+Healthy with all resources
   healthy. Check: `scripts/9669-g8-argocd-check.sh` exit 0, output pasted.
5. Bounded re-verification. Same probes as acceptance, nothing more:
   unauthenticated edge `/health`, one authenticated read, MCP `tools/list`
   (expect 401 unauthenticated where the surface requires auth). Check:
   `scripts/9669-g8-bounded-reverify.sh` exit 0 with status codes pasted.
6. Restore the room. Return pins and branches to their pre-rehearsal state;
   leave no port-forward, no live flag, no traffic shift. Check: post-run
   digest capture equals the pre-run capture and `git status` is clean.

## Evidence packet (what G8 closure cites)

- Named holder and rehearsal date/window on #9669.
- Base pair (core + satellite SHAs) and removal-commit SHA reverted.
- Last-good digests before and after, byte-identical.
- ArgoCD Synced+Healthy output at restore time.
- Bounded re-verification output: `/health`, the one authed read, MCP
  `tools/list`, each with status code and correlation id where emitted.
- Elapsed time per step and total.
- Statement that cluster state, schema, and traffic routing are untouched
  beyond the rehearsal target, with the post-run digest comparison as proof.

## Void triggers

- The base, deletion set, pins, or generator change after the rehearsal.
- The revert needs conflict resolution (the run proves nothing; re-freeze
  and re-run).
- Any step runs against a different base or head than the recorded pair.
- D12 or #474 evidence is cited as G8 evidence (ruled out by the arbiter).

## Waiver path

G8 closes without execution only through the Plan B waiver recorded in
[9669-g8-plan-b-waiver-template.md](9669-g8-plan-b-waiver-template.md):
an explicit owner waiver quoted on #9669 plus a filed revival issue, valid
only when the removal PR carries no irreversible state or schema change.
