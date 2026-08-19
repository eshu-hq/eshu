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
same gate on a stale doc sentence as on a guard that can assert the wrong thing,
and it blocks sibling PRs and other sessions waiting on the merge.

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

## Findings introduced by review fixes

Count these separately and say so in the verdict. A fix-induced P2 is evidence
the fix widened scope, and it is the signal to stop fixing inline and defer,
not to start another round. Two consecutive rounds whose only new findings are
fix-induced means the diff is converged: land it and track the remainder.

## Repeat findings

A finding restated across rounds is one finding, not a new one. Reference the
prior round, do not re-derive it, and escalate rather than re-litigate: if it
was blocking then and is unfixed now, it is still blocking and the PR is not
ready. If it was dispositioned, the disposition stands unless the diff changed
underneath it.

## Stating it

The verdict carries `P0`, `P1`, `P2-blocking`, and `P2-deferred` counts.
"Ready" means `P0=0`, `P1=0`, `P2-blocking=0`, every deferred P2 tracked and
named, and the owner able to see what was deferred and why. Never report a
deferred finding as absent.
