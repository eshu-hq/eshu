# Sustained Drives And Waiters

For a user-requested persistent goal, the owner can use a prompt such as:

```
/goal Drive issues <list> to fully closed per the eshu-issue-driver skill —
load the eshu-issue-driver skill now and follow it. Not done until every proof
clause in that skill's Completion Evidence section is satisfied. Stop after 50 turns if
blocked only on operator-side action (say so).
CONSENT: push, pr-open
```

The `CONSENT:` line is the owner granting those irreversible acts up front;
existing explicit session authorization also applies. Ask only for an act
not already authorized; never write a consent grant on the owner's behalf.
Grant only the acts you mean. **Add `merge` to that line** if you want the drive to land the PR in
the merge step unattended — it is left out of the template deliberately, because a
merge is the least reversible act in the canon's list and the one nobody
reviews afterwards, and a copy-pasted default is not the place to grant it.

Use `/goal` only when the user requests a goal; invoking this skill alone does
not create one. The `/goal` evaluator reads the conversation, including this skill,
so "done per the skill" is checkable. Use the available goal/wait facilities
within active harness limits. While a PR is open, poll conflicts, CI, and reviews about
every 60 seconds; do not only wait for the check rollup.

Poll with a **bounded background waiter that blocks until a condition holds**
(`until <check>; do sleep 40; done`, with an iteration cap), run as a background
command — a foreground sleep is refused by the Claude Code harness — not by
spending a turn per poll. One waiter per condition: duplicates racing on the same
condition waste turns, and a waiter whose match pattern cannot occur — watching a
log for a string that run never prints — spins to its cap while reporting
nothing. Stop your own superseded waiters when the thing they watch is replaced. The
cadence is a ceiling on staleness, not a requirement to burn a turn each minute.

Confirm a detached launch actually took, by its process, not by the shell's exit
status: `setsid` does not exist on macOS, so `setsid nohup … &` backgrounds an
instant failure and returns 0. The log you then read is a stale file from an
earlier run, which reads exactly like progress. Check the log's mtime and its
size delta before believing its tail — mtime alone can advance on a process
stuck retrying, so size delta is the better liveness probe.
