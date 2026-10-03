# Writing A `/goal` Condition

This page belongs with [Agent Hooks](agent-hooks.md). Read it before you write
a `/goal` condition.

The `/goal <text>` command also registers a Stop condition at session level.
A model judges that condition against the transcript. It does not read the goal
file, so a `DONE:` first line does not satisfy it. In one session the judge
refused a stop more than ten times in a row on a condition that the work could
not meet, because the fix already existed upstream. Each refusal cost one full
turn and ended on the same facts.

Write the condition so that every honest outcome can satisfy it:

- Name the outcome, not the route. Write "issue N is fixed on main, shown by a
  failing test or an existing fix with evidence", not "open a PR for issue N".
- Add the already-delivered case: "or show the fix is already on main with
  RED and GREEN evidence".
- Do not list an action (push, merge) as the condition. An action cannot be
  done when the work needs none.

Stop only when the whole job is verified complete (see "Mandatory Startup" in
`AGENTS.md`). If it is complete and the judge keeps refusing a stop that the
facts support, do not invent work to satisfy it. Report the facts, keep the
`DONE:` line, and ask the owner to run `/goal clear`. If the job is not complete,
keep working.

The count of refusals above comes from one session and is not a measured rate.
For the built-in behavior of `/goal`, read the Claude Code documentation at
https://code.claude.com/docs/en/goal. This page does not describe it fully.
