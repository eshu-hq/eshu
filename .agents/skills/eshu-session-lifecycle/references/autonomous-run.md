# Autonomous run

You own the exit condition. Define done, then drive to it without stopping.

Triggers: "run until done", "going to bed", "keep going", "don't stop", a
`/loop`, or any goal handed over without a checkpoint schedule.

The other four playbooks in this skill are about stopping well. This one is
about not stopping. It does not loosen a single gate: everything in
Prove-The-Theory-First, the evidence rules, and the pre-PR ladder still applies
at full strength. Autonomy is about who answers the question, not about whether
the proof gets run.

## Steps

1. **Write the exit condition as a checkable predicate before iteration one.**
   Not "improve the gate", but "`scripts/verify-x.sh` exits 0 on a cancelled
   bucket". A vague goal stalls, because nothing tells you when to stop. If you
   cannot state the predicate, that is the first thing to go research.

2. **Route every question by why it is unclear, not by how unsure you feel.**
   Self-assessed certainty is the test that already failed; these are
   observable.

   | Why it is unclear | Move |
   |---|---|
   | A committed fact settles it — code, a local doc, an ADR, a measurement, a cheap experiment | **Research it**, cite the settling evidence, proceed |
   | Owner, design intent, performance contract, or verification gate is unsettled | **Ask** |
   | Complete evidence would still leave a product-taste or business call | **Ask** |
   | The next act is irreversible | **Ask** — unless the owner already granted that act durably (`CONSENT:` in the goal file, or `CLAUDE_GOAL_CONSENT`), in which case do it and say you did |

   Architecture is not itself an anchor. Under a settled design intent, an
   architecture question is a research task, and settled intent admitting two
   defensible implementations means dispatch an adjudicator rather than park the
   work — see
   [Delegate An Undecided Design](../../../docs/internal/agent-guide.md#delegate-an-undecided-design-do-not-escalate-it).
   Hand it the symptom and raw observations, labelled as a guess, never your
   hypothesis as fact.

   Two rules keep this honest. **Cite the fact that settled it**, so a bad
   routing decision is visible at review rather than buried in confidence. And
   **time-box the research** — this router trades one stall mode for another,
   and an unbounded investigation blocks you just as effectively as a question
   nobody answered.

   **What already counts as settled intent.** Agents routinely treat a
   well-specified task as unclear and ask anyway. These are evidence, not
   context; if they decide the question, you have your answer:

   - the issue body and its acceptance criteria, including a stated preferred
     direction
   - an ADR, design doc, or plan under `docs/internal/`
   - the Life Motto's ordering — accuracy first, then performance, then
     concurrency — which settles most "which of these matters more" questions
   - the ownership table and the performance contract for the touched surface
   - how the surrounding code already does it

   **A recommendation is a judgment, not a request for validation.** If you have
   researched something well enough to recommend it, you have researched it well
   enough to do it. Writing "I'd suggest X — shall I?" on reversible work is the
   same stall as asking outright, with extra steps. Do X, say you did it and
   why, and name what would change your mind.

   Every genuine Ask still arrives carrying the researched recommendation.
   Asking is never a substitute for having looked.

3. **Proceed on anything reversible.** In this repo that means: editing files in
   your own worktree, committing to your own branch, creating and removing your
   own clean worktree, running any gate or test, regenerating artifacts inside
   your worktree, and reading anything at all.

   Stop and ask for these, every time, no matter how confident you are:

   - `git push`, force-push, or anything that moves a remote ref
   - `gh pr create`, `edit`, `merge`, `comment`; `gh issue create` or `comment`
   - deleting a worktree that holds uncommitted work, or any worktree that is
     not yours
   - any write to the main checkout
   - deploys, remote-host actions, and teardown of a Compose stack you did not
     start
   - **changing the golden standard** — the cassettes under `testdata/cassettes/`
     or the B-12 snapshot — to match a design you researched rather than one the
     owner settled
   - anything a person outside this machine would see

   That golden-standard entry is not a pre-merge/post-merge distinction, and it
   is the one exception worth understanding rather than memorising. The safety
   story for solo work is "a wrong design gets caught by the gates". But
   `eshu-golden-corpus-rigor` requires the cassettes and snapshot to move in
   lockstep with projected truth, so an agent confidently building the wrong
   thing will also update the artifacts that would have caught it. After that
   the gate floor defends the wrong design and every check is green. The only
   remaining catch is a human reading the PR — which is exactly the safety net
   autonomy is supposed to stop leaning on.

4. **Mid-run discoveries are yours.** A broken gate, a flaky verifier, stale
   docs, a bug adjacent to the one you came for, drift you can fix — fix it.
   Do not park reversible work for the human and do not file an issue instead of
   fixing it. Keep an out-of-band fix in its own commit so the diff stays
   readable, and return to the predicate afterwards.

   The exception is the same as everywhere else: if the fix needs a design
   decision only the owner can make, or crosses into another agent's active
   surface, that is bucket two.

5. **Verify each unit before starting the next.** Small steps, each proven, is
   what makes an unattended run auditable. Batching the checks to the end means
   a failure at hour three has three hours of unverified work behind it.

6. **Checkpoint every iteration.** One line: what changed, and whether the
   predicate moved. A run with no trail cannot be resumed or audited, and
   resuming it becomes a session-pickup with nothing to pick up.

7. **A plateau is not a stop.** If progress stalls, change approach rather than
   stopping to report the stall. Surface a genuine dead end — a blocked
   credential, a contradiction in the requirements, an irreversible fork — not
   the fact that the obvious thing did not work.

8. **Never relax the predicate to declare victory.** Moving the goalposts at
   hour four is the single most expensive failure this playbook can produce,
   because it arrives wearing the costume of a finished job. If the predicate
   turns out to be wrong, say so explicitly and stop; do not quietly swap it.

## Reply

Your report must contain:

- the exit condition, stated as the predicate you committed to at the start
- iterations run, and what landed in each
- out-of-band fixes made along the way, with their commits
- what you discarded, and why
- final predicate state: met, or not met with the reason
- anything you surfaced instead of deciding, and what it is waiting on

## What still stops you

Autonomy is not a proof exemption. A run that ends with the predicate met but
the gates unrun has not met the predicate. The pre-PR ladder in particular is
untouched: local proof, then a clean review, then `make pre-pr`, then a final
review, and the push itself is still bucket three.
