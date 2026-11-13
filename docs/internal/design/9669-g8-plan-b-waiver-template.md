<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# G8 Plan B waiver template (#9669)

Refs #9669, part of #6707. Status: template only — no waiver is granted by
this file. Copy both blocks onto #9669 when the owner orders the waiver path.
G8 closes on waiver only with both blocks present and with no irreversible
state or schema change in the removal PR.

## Block 1 — owner waiver quote (paste verbatim on #9669)

> Owner Plan B waiver for 9669-G8 (date: YYYY-MM-DD, owner: @handle):
> "G8 live revert-removal rehearsal is waived for the removal base
> <core SHA> / <satellite SHAs>. Reason: <one or two sentences>.
> The removal PR <number> carries no irreversible state or schema change.
> Rollback, if ever needed, follows <revival issue number>."
>
> Recorded by: <coordinator handle>. Source: <comment URL>.

Rules: the quote names the exact base pair and the removal PR; a standing or
forward-dated waiver is not valid. A base, scope, deletion-set, or pin change
after the quote voids it.

## Block 2 — revival issue shape (file first, then link from #9669)

Title: `9669 revival: restore <surface> after removal at <core SHA>`

Body:

```markdown
Refs #9669. Part of #6707.

Removal base: core <SHA>, satellites <SHAs>, removal PR <#>.
Last-good artifact: <repo:tag@digest> containing go/cmd/api.

Revival steps:
1. `git revert <removal-commit-SHA>` on a fresh branch (clean apply only).
2. Rebuild and re-serve the restored core artifact via the standard
   release path; re-pin to the last-good coordinates above.
3. ArgoCD Synced+Healthy; paste the check output.
4. Bounded re-verification: edge /health, one authed read,
   MCP tools/list; paste status codes.
5. Record elapsed time; restore the room (pins, branches, forwards).

Acceptance: digests match last-good, ArgoCD green, bounded probes pass,
state otherwise untouched, elapsed time recorded.
```

Labels: the same area label the removal PR carries. Assignee: the G8 holder
named on #9669. The issue stays open until the removal PR merges or the
waiver is superseded by a live rehearsal.
