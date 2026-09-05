# Performance Target Triage

## Target Contribution Budget

Before prioritizing or dispatching a candidate, calculate and record:

- `current_total_seconds`: the accepted baseline's primary metric;
- `target_seconds` and `required_saving_seconds` (`max(current - target, 0)`);
- the candidate stage and its measured seconds;
- `maximum_recoverable_seconds`: the theoretical ceiling for that stage;
- `expected_saving_seconds`: the realistic win supported by the shim;
- `minimum_worthwhile_saving_seconds`; and
- the expected margin above or below the required target gap.

Do not prioritize a candidate that cannot theoretically recover the target gap
when another measured critical-path candidate can. Do not spend an
operator-scale run on an expected saving below the worthwhile threshold. A
small optimization may still proceed for a separately stated SLO or resource
goal, but it must not be presented as the path to the end-to-end target.

## Open Issue Performance Triage

Before choosing an open performance issue, re-rank it against current evidence.
Issue titles, old severity, and stale long-pole claims are not priority data.
Use the latest accepted baseline, phase timings, target gap, and correctness
blockers as the source of truth.

Classify each candidate before implementation:

- `strategic-target-work`: its measured or theoretical ceiling can materially
  close the active target gap.
- `hygiene-cleanup`: the issue is real and worth doing, but its expected saving
  cannot materially move the active wall-clock target.
- `diagnostic-only`: the work improves attribution or operator evidence without
  claiming a speedup.
- `correctness-blocker`: graph, storage, queue, or query truth is suspect; fix
  this before publishing or celebrating speed.
- `superseded`: newer proof or a merged fix changed the bottleneck or eliminated
  the reason to implement the issue as written.

If the candidate is `hygiene-cleanup`, do not use the main performance cycle on
it while a measured critical-path candidate exists. If newer measurements
contradict the issue body, update the issue with the new classification and the
evidence before dispatching implementation.

## Performance Orchestration

Use subagents or role agents whenever the work can be safely split into bounded
read-only or mechanical lanes, such as live GitHub issue-state inspection,
route/caller inventory, old/new query-shape proof, test-surface discovery, and
verification-log review. Each subagent handoff must include the relevant
project skills, exact surface, proof task, and out-of-scope boundary.

Do not dispatch implementation subagents for unproven optimization theories.
First prove or reject the theory with the cheapest representative shim. Once
the proof plan and acceptance packet are clear, route bounded TDD
implementation to an execution-focused agent.

Use a capable diagnostic model for bottleneck attribution, query-plan
interpretation, and proof-priority judgment. Respect the user/runtime selection;
model configuration belongs to the runtime, not a version pinned by this skill.

## Cost-Aware Diagnostic Dispatch

Reserve diagnostic reasoning for bottleneck localization, hypothesis
design, profile/query-plan interpretation, and proof judgment. Once the theory
and implementation packet are complete, stop that diagnostic task. Use an
execution-focused model for bounded TDD implementation, and use scripts or the
coordinator for builds, routine polling, CI watching, GitHub bookkeeping, and
cleanup. Do not spend frontier reasoning tokens babysitting a long run.
