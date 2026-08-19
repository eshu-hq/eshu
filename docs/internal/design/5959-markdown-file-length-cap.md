# Decision: the 500-line file cap stays Go-only

**Decided 2026-08-19. The cap is not extended to Markdown.**

## Where this decision came from

#5959 split `go/internal/collector/README.md` under the Go cap and, on closing,
left the general question open:

> The secondary question this issue raised — whether the 500-line cap should be
> mechanically enforced for Markdown as well as Go — is not resolved here.
> `tools/golangci-lint-filelength` walks the Go AST and structurally cannot see
> `.md` files. If that enforcement is wanted it needs its own issue rather than
> keeping this one open.

**The answer is that it is not wanted, so no issue was opened.** That comment
asked for an issue conditionally — if enforcement is wanted — and a tracking
issue for work nobody intends to do is worse than a written decision.

**The decision was given directly by the repository owner, in a working session
rather than on a GitHub thread, so there is no issue comment to point at.** That
is worth stating plainly: someone auditing this later will find no sign-off in
the tracker and should not conclude the decision was invented. The measurement
and the reasoning below are mine. The call — do not enforce — is the owner's,
and if that attribution is wrong this is the line to correct.

## What the cap covers today, and why the gap is real

The 500-line cap is Go-only by construction, in three independent places:

- `.pre-commit-config.yaml` declares `types: [go]` on the hook.
- `scripts/dev/precommit-go.sh` walks `git ls-files 'go/*.go'`.
- The CI implementation is a golangci-lint AST plugin. It parses Go. It cannot
  see a Markdown file at all.

So this is not a rule with an oversight in its enforcement. Markdown was never in
scope, and no amount of fixing the hook would change that without a second,
differently-built checker.

## The exposure, measured

Twenty files under `go/` exceed 500 lines. Measured at `7d0146810` with:

```
for f in $(rg --files -g 'go/**/*.md'); do
  n=$(awk 'END{print NR}' "$f"); [ "$n" -gt 500 ] && echo "$n $f"
done | sort -rn
```

| Lines | File |
| ---: | --- |
| 3,766 | `go/internal/storage/postgres/README.md` |
| 2,401 | `go/internal/reducer/AGENTS.md` |
| 1,924 | `go/internal/storage/cypher/README.md` |
| 1,414 | `go/internal/query/README.md` |
| 1,172 | `go/internal/storage/postgres/AGENTS.md` |
| 1,159 | `go/internal/query/evidence-notes.md` |
| 1,032 | `go/internal/storage/postgres/evidence-notes.md` |

Thirteen more sit between 501 and 901 lines.

## Why not enforce

**The cost is a project, not a chore.** Twenty splits, or a twenty-row
grandfather list. Neither is something a contributor picks off a board between
tasks, and an unowned rule with a twenty-row exemption list teaches people that
the exemption list is where their file goes.

**The grandfather mechanism has a known contention cost.** The `dirgate`
grandfather pin already serialises extraction PRs: every extraction re-pins the
same row, so two of them cannot land independently. Adding a second grandfather
ledger of the same shape would extend that serialisation to documentation edits,
which are otherwise among the cheapest changes to land.

**The Go cap and a Markdown cap are not the same rule wearing different file
extensions.** The Go cap exists because a 500-line Go file is hard to review and
hard to own. A long `README.md` is bad for a different reason — it is usually
under-organised rather than over-long — and the fix is headings, splitting by
audience, or moving evidence out to `evidence-notes.md`, not a line count. A line
cap would push authors toward whichever split makes the number go down, which is
not reliably the split that makes the document readable.

## What would reopen this

- The package restructure under epic #6053 landing, if it leaves per-package
  documentation that a line cap would now help rather than hinder. That epic
  governs directory file **counts**, not file length; it is named here as a
  trigger to revisit, not as the issue this decision belongs to.
- Evidence that long documents are actually costing review or onboarding time,
  as opposed to being merely conspicuous.
- A checker that measures something more useful than lines — heading depth,
  section length, or duplicated content across `README.md` and `AGENTS.md`.

Until then: no issue, no gate, no grandfather ledger. The three worst files are
worth splitting on their own merits whenever someone is editing them anyway, and
that is a judgement call rather than a rule.

Refs #5959, which raised the question and asked for it to be answered elsewhere.
