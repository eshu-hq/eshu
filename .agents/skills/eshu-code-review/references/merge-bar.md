# Merge Bar: When A P2 Blocks

`P0=0` and `P1=0` are absolute. This file defines when a **P2** blocks, because
an unqualified "fix every P2 before push" has no terminator and has repeatedly
stalled ready work.

## The failure this exists to stop

A full review of a non-trivial diff can nearly always produce another P2. Worse,
the fixes themselves generate them: on one PR, two of three late-round P2s were
introduced by earlier review fixes, each fix widening the surface the next pass
inspected. Each round was individually correct and the PR still could not
converge. Reviewers were not wrong; the bar was unbounded.

Treating every P2 as blocking also inverts severity in practice. It spends the
same gate on a stale doc sentence as on a missed empty-input edge case, and it
blocks sibling PRs and other sessions waiting on the merge. (Both of those are
genuinely P2. A guard that can assert the wrong thing is a false-green test,
which the severity table classifies P1 — it was never deferrable.)

## The bar

A P2 **blocks** when either is true:

1. **It contradicts a claim the PR itself makes.** The PR body, its title, or a
   review reply asserts something the finding shows to be false. A PR claiming
   byte-identical parity has a blocking P2 the moment a behavioural difference
   is found; a guard PR claiming it binds production to a query has a blocking
   P2 if the guard can bind the wrong one.
2. **It is cheap and in the same edit.** Same function, same file, same evidence
   path, and fixable without new proof. Defaulting to `fixed` remains correct —
   this bar bounds the loop, it does not license deferral.

A P2 **does not block** when it is genuinely orthogonal to the PR's claims,
needs its own proof or a design decision, or would only be reachable by widening
scope. Disposition it `deferred-to-linked-follow-up`, state it in the PR, and
merge.

**Apply both criteria before considering deferral, and record the result.**
Deferral is only available once the finding has failed criterion 1 and
criterion 2, so an agent that reaches the owner-agreement paragraph without
having tested them is asking about a branch that may not be open. State, for
each criterion, whether the finding failed it and why; naming one criterion
leaves the gate half-tested, and "it is a P2" tests neither.

One exception, and only one: the fix-induced convergence rule below. A P2 that
a review fix introduced may be deferred even when criterion 2 would hold it,
because that rule exists to terminate a loop this gate would otherwise make
endless — each fix widens the surface, the next pass finds another cheap
same-file P2, and criterion 2 blocks it forever. Convergence beats the gate
there by design. Everywhere else the gate stands.

This ordering is load-bearing because the failure it prevents has happened. A
review of the #6108 work found that the PR's new "scope honesty" section framed
two counters as measuring a blind spot while a third population was measured by
neither, then put the deferral question to the owner with "file a follow-up" as
its recommendation. That finding contradicts a claim the PR itself makes, which is
criterion 1 exactly. It blocked, there was no follow-up to file, and no
question to ask — the agent had both the rule and the contradiction in its own
words, and reached the deferral paragraph without passing through the test
upstream of it. Read the two criteria as a gate on the paragraph below them,
not as background for it.

Deferral is not the reviewing agent's call alone. It requires **the owner's
agreement, quoted in the PR**, exactly as `SKILL.md`, `eshu-issue-driver` Step 6
and the root canon already demand — an exception the invoking agent can
self-certify is not a gate, and this bar must not become one. "Tracked" means a
linked follow-up issue, not a sentence in a PR body: without an issue, nothing
is tracked.

Owner agreement is universal: there is no category a reviewing agent may defer
on its own reading. An earlier draft carved out doc/naming/minor-perf findings,
which re-introduced exactly the self-judgement this bar is meant to bound.

Two rules keep the judgement that DOES stay with the agent auditable:

- **Every deferred P2 carries its severity-table category verbatim** in the PR
  ("doc drift", "edge case", "genuine missing coverage", …) **next to the
  finding text**. The category alone is a label the agent chose; the pair is
  checkable, because the owner reads what the finding actually says beside the
  grade claimed for it. The blocking test is auditable for the same reason —
  the finding, the PR's claims and the diff all sit in the PR.
- **The owner may re-grade severity when they agree, and the re-grade stands.**
  Without this the owner is agreeing to a deferral, not to a severity, and
  P1-vs-P2 is the call that now decides push and merge. An agent whose "naming"
  finding is re-graded P1 fixes it; it does not re-argue the grade.
- **A PR that claims little does not thereby lower its own bar.** "Contradicts a
  claim the PR makes" is read against what the PR *should* assert for its
  change — the description-carries-the-evidence rule — not against a
  deliberately thin description.

## Findings introduced by review fixes

Count these separately and say so in the verdict. This section is the single
exception to the deferral gate above: a fix-induced P2 may be deferred without
failing criterion 2, because the alternative is a loop with no terminator. A
fix-induced P2 is evidence the fix widened scope, and it is the signal to stop
fixing inline and defer, not to start another round; prefer reverting the
widening fix over leaving it together with the finding it induced. Two
consecutive rounds whose only new findings are fix-induced **P2s** means the
diff is converged: land it and track the remainder. A fix-induced P0 or P1 is
never covered by this — those stay absolute.

## Sweeping rule text

Any claim that a phrase is gone everywhere is a claim about the whole corpus,
and rule text is line-wrapped. `rg` matches per line, so **run every sweep with
`-U`**: a clause broken across a wrap is invisible to a single-line pattern at
any breadth. Positive-control the pattern against the base first — if it finds
fewer sites there than you are about to edit, it is blind before its zero on
HEAD means anything — then read a deliberately over-broad net, also with `-U`,
because a per-line loose net inherits the exact blindness it exists to
compensate for. State how the claim was established beside it: a labelled count
invites correction, "I swept it" asks for trust.

## Repeat findings

A finding restated across rounds is one finding, not a new one. Reference the
prior round, do not re-derive it, and escalate rather than re-litigate.
Escalate means: say it is a repeat, name the round it first appeared in, and
raise it to the owner once it survives two rounds unfixed. If it was blocking
then and is unfixed now, it is still blocking and the PR is not ready. If it was
dispositioned, the disposition stands unless the diff changed
underneath it.

## Stating it

The verdict carries `P0`, `P1`, `P2-blocking`, `P2-deferred` and `P3` counts.
"Ready" means `P0=0`, `P1=0`, `P2-blocking=0`, and every deferred P2 tracked in
a linked issue with the owner's agreement quoted in the PR, named there with its
severity-table category, so the owner can see what was deferred and why. Never
report a deferred finding as absent.

A non-zero `P3` does not affect readiness and needs no issue. It is reported so
the count is honest about what the review saw, and so a reader can tell a diff
with four cosmetic findings from one with none. Do not suppress P3s to make a
verdict look cleaner: a review that hides what it noticed is the same failure as
one that overstates what it proved.
