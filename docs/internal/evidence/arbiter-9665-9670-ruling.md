<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2026 eshu-hq -->

# Arbiter ruling: #9665 deferral and #9670 close (goal drive 9665/9669/9670/9671)

Role: `arbiter-eshu`. Verdict lines for the goal ledger, verbatim:

- `DEFER: #9665 docs/internal/evidence/arbiter-9665-9670-ruling.md`
- `CLOSE: #9670 per arbiter ruling docs/internal/evidence/arbiter-9665-9670-ruling.md (AC5 met by disposition, not by all-green; residual red owned by #9731)`

## Decisions

1. **#9665 — writing is not rehearsing; #9383 requires this issue to exist.**
   #9383 (CLOSED) offers each boundary two dispositions: a rehearsed
   rollback, or an owner roll-forward waiver **plus a filed revival
   follow-up**. #9665 is that follow-up for #8876. The runbook states the
   operative policy as "Eshu does not **rehearse** downgrades for this
   boundary" (`docs/public/operate/webhook-satellite-roll-forward.md:14`).
   Nothing in #9383 or the runbook forbids *writing* a revival procedure.
   Closing #9665 as wontfix under #9383 would destroy the waiver's own
   condition.

2. **#9665 criterion-4 disposition: DEFERRED-WITH-RULING, converting to a
   merge gate.** Criterion 4 ("closed or deferred with a linked ruling before
   it blocks any removal") is satisfied by its deferral branch via this
   ruling, linked from #9665 and the goal ledger. Criteria 1–3 stay unchecked
   and are not workable now: the exact pre-removal revision does not exist
   until the removal PR fixes its base, and there is nothing to revive while
   the listener is in tree — verified files under
   `go/cmd/webhook-listener/` at `origin/main`, provider endpoint not yet
   moved (runbook:20), epic #8876 OPEN. Concretely: #9665 blocks nothing in
   #8876 up to and including the authority switch (listener still in tree =
   switch-back is trivially available). It becomes a hard gate at exactly one
   point: **the #8876 removal PR may not merge unless that PR (a) names its own
   base SHA as the pre-removal revision, (b) lands criteria 1–3 in the
   roll-forward runbook in the same PR, and (c) closes #9665 or carries it
   forward with a named owner.** Writing 1–3 earlier with a placeholder SHA
   would be stale on arrival; writing them after merge leaves an uncovered
   window. Same-PR is the only accurate point.

3. **#9670 AC1–AC4: met.** AC1/AC2: PR #9679 (MERGED, merge commit
   `cf357fcf`) carries the consumer-by-consumer census; independently
   re-verified at current `origin/main`: `go/cmd/mcp-server/` = 0 files,
   survivor `go/internal/mcp/` present, installer builds the pinned satellite
   (`scripts/install-local-binaries.sh:72`), `service.go` satellite error text
   present (`go/cmd/eshu/service.go:171`), evidence note
   `docs/internal/evidence/9670-mcp-server-removal.md` in tree. AC3:
   retired-coverage honesty note is a named section of the PR body (171-tool
   census, deleted vs still-guarding suites). AC4: both linked comments exist
   (6532716872, 6532975082 on #9521, verified via API; rev-109 live recovery +
   rehearsed removal half). AC4 as written points at those URLs, so they
   satisfy it.

4. **#9670 AC5: met by disposition, not literally — and the close comment must
   say so.** "Full gates green" read literally is false: PR head reports 164
   checks = 146 success / 14 skipped / 1 FAILURE (MCP-identity E2E) / 1
   StatusContext `required-gates-complete` FAILURE (disabled aggregate),
   stable x2. But the operative merge bar is `go-core-complete` +
   `go-race-complete` green with no completed-red left undispositioned — both
   SUCCESS on the #9679 head. The CheckRun red was dispositioned *pre-merge*
   in the PR body ("Known CI reds"): root cause outside the diff (satellite
   has no JWT validation; E2E stack never sets the issuer env; SSO leg green;
   image serves 171 tools), tracked in #9731 (OPEN). Review threads
   independently verified: 9 total, 0 unresolved. Keeping #9670 open cannot
   unmerge `cf357fcf` or turn that check green — the residual work belongs to
   #9731, not to the in-tree removal. Close is therefore correct, provided the
   close comment records AC5 as "merged under the merge bar with 1 admitted
   FAILURE owned by #9731" and never claims all-green.

5. **Scoped post-merge proof is the right bar for closing #9670; a full
   local-testing run now would prove nothing about #9670.** PR-era proof was
   broad (build, 57 packages, registry/moved-refs/telemetry/docs gates, per PR
   body). The close audit needs the removal's invariants on current main, which
   are scoped by nature: `go vet ./internal/mcp/...` exit 0;
   `go test ./internal/mcp/ -run 'Census|ToolRegistry'` ok, exit 0. A full
   suite on today's main would attribute other lanes' changes to #9670 either
   way. Accusing the close of insufficient proof on that basis is rejected.

6. **Stale prose: the real residue is a follow-up, not a close blocker.**
   Verified at `origin/main`: `go/cmd/README.md:19` already says "External
   satellite"; `go/cmd/eshu/AGENTS.md:134` correctly describes the satellite
   binary-not-found path matching `service.go`. Only the telemetry lines
   (`docs/public/reference/telemetry/index.md:637`, 659) describe the deleted
   in-tree transport middleware in present terms — P3 prose-form. AC5's "docs
   updated in the same PR" was honored; stale sentences do not make the removal
   incomplete. Condition on close: file or link a follow-up issue for the
   telemetry wording and name it in the #9670 close comment. Do not reopen
   #9670 for it.

## Required proof from the coordinator

- Post this ruling (or its file path) as a comment on #9665 and add the
  `DEFER:` ledger line; add criteria 1–3 as a checklist on the future #8876
  removal PR template/body when opened.
- #9670 close comment must contain: the 5 AC with evidence pointers; the AC5
  honest disposition (146/14/1 admitted FAILURE owned by #9731; StatusContext
  `required-gates-complete` FAILURE is the disabled aggregate, not a required
  gate); the required gates `go-core-complete` + `go-race-complete` SUCCESS on
  the merge head; and the telemetry-prose follow-up issue link.
- When the #8876 removal PR opens, quote Decision 2's three merge conditions
  in its body.

## What the arbiter will not do, and why

- No downgrade/revival **rehearsal** for #9665 now: it rehearses reviving code
  that is still in tree, against the owner roll-forward-only policy, and
  proves nothing about a future pre-removal revision.
- No keeping #9670 open until the MCP-identity E2E is green: that conflates
  satellite JWT scope (#9731) with the completed in-tree removal and leaves a
  merged removal's issue permanently hostage to another lane.
- No "full gates green" language anywhere in the #9670 close record: it is
  factually false (Decision 4).

## Unverified (NOT_CHECKED)

- PR #9679 check-by-check list: `gh pr checks 9679` hit a GraphQL rate limit
  in the arbiter session. The 164-check breakdown is brief-supplied,
  corroborated by threads (9/0, independent), #9731 OPEN, and the PR body's
  own reds section. Coordinator re-verified the two required gates
  (`go-core-complete`, `go-race-complete` SUCCESS) and the StatusContext shape
  before closing.
- Full-suite state of current `origin/main`: not run, and per Decision 5 not
  required for either ruling.
- Live QA satellite serving state today (rev 111 claims are from the #9521
  comments, read but not re-probed).
