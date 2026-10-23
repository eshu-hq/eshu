# Hosted MCP tool-surface validation with real agents (#8760)

**On one hosted QA build, the `router` surface had the best first-call accuracy
for both clients and used about one seventh of the input tokens for Claude Code,
but it did not match `full` on answered questions for Claude Code. The refreshed
`core_router` was the weakest surface on both clients.** This note holds the
numbers, the scoring rules, a 22-run manual check of the scoring, and the gaps.
It supports the default-surface decision in epic #8752. It does not make that
decision.

| At a glance | |
|---|---|
| Build | `c5f784d73c`, one deployed build for every run, zero run errors |
| Clients | Claude Code 2.1.309 (model `sonnet`) and codex-cli 0.160.1 (model `gpt-6.1-sol`) |
| Matrix | 2 clients x 3 surfaces x 26 tasks x 3 repeats = 234 runs per client |
| Headline | `router`: best first call on both clients, lowest tokens; `full`: most answers; `core_router`: worst on both |
| Scoring check | In a 22-run manual sample, the scored success matched my reading of the answer in 13 runs (59%) |

## What was measured

A hosted QA deployment served the MCP endpoint. Each run started a real agent
client in an empty directory, pointed it at the endpoint with `?surface=full`,
`?surface=router`, or `?surface=core_router`, and asked one question. The three
surfaces share one server, so the build, data, and index state are identical
across surfaces.

The harness is `scripts/dev/mcp-surface-eval/`. `run_eval.py` drives the client,
`score.py` scores the event stream, and `report.py` renders the tables. Run
order is shuffled with seed 8760, and 4 runs execute at a time. Claude Code runs
with its built-in tools off and only the Eshu server allowed, with a 15-turn
cap. Codex runs in a read-only sandbox.

The 26 tasks are two per corpus family across all 13 families of the router
eval corpus (`go/internal/mcp/router/eval/testdata/corpus.json`). The
questions are written against real entities in the QA deployment, so they are
org-realistic and stay private. The repository holds corpus ids and family
labels only.

## Scoring rules

- A call is an MCP call to the Eshu server. Calls to anything else are counted
  separately and never score.
- For `call_tool`, the target is the wrapped tool name. `find_tool` is a
  discovery hop. It counts as a call but is never a wrong tool.
- A wrong pick is a domain call whose target is neither the corpus expected
  tool nor one of its alternates. It is split into a helper (a lookup that
  resolves identifiers, such as listing repositories), a sibling (another tool
  from the same corpus family), and an off-family tool. Only sibling and
  off-family picks count as genuinely wrong.
- The genuine wrong-pick rate is wrong picks divided by domain calls, so a
  surface cannot look better by making fewer, wrong calls.
- A result the client could not read is not an answer. That covers the older
  "exceeds maximum allowed tokens" error and the newer notice where Claude Code
  swaps a large result for a 2 KB preview and a file path the tool-less agent
  cannot open. A server error such as a response over the byte budget is also
  not an answer.
- "Expected tool answered" means an expected or alternate tool returned a
  readable, non-error result and the agent wrote a final answer.

That last measure is a proxy. It does not grade the answer text, and a question
with no data can still succeed. The manual check below measures how far the
proxy can be trusted.

## Results

### Tool list size

| Surface | Tools advertised | `tools/list` bytes |
|---|---|---|
| `router` | 2 | 1,543 |
| `core_router` | 12 | 17,718 |
| `full` | 163 | 209,573 |

These were measured live against the deployed build. `core_router` is
`find_tool`, `call_tool`, and the 10 core tools from #8759.

### Summary by client and surface

All 26 tasks, 78 runs per row. "Genuine wrong-pick rate" and "Helper-call rate"
are shares of domain calls. Cost is not reported by Codex.

| Client | Surface | Runs | Mean tool calls | Genuine wrong-pick rate | Helper-call rate | First call correct | Expected tool answered | Median wall s | Mean input tokens | Mean cost USD |
|---|---|---|---|---|---|---|---|---|---|---|
| claude | full | 78 | 3.81 | 40% | 10% | 95% | 87% | 11.2 | 330584 | 0.083 |
| claude | router | 78 | 3.91 | 28% | 5% | 99% | 82% | 12.9 | 44743 | 0.051 |
| claude | core_router | 78 | 4.45 | 49% | 6% | 73% | 73% | 14.0 | 85966 | 0.063 |
| codex | full | 78 | 7.65 | 69% | 8% | 76% | 92% | 21.4 | 250997 | n/a |
| codex | router | 78 | 11.01 | 54% | 4% | 87% | 90% | 22.9 | 202653 | n/a |
| codex | core_router | 78 | 11.69 | 54% | 5% | 65% | 82% | 21.7 | 211549 | n/a |

### Method and corrections: the Claude Code scoring defect

The first scoring pass missed the newer Claude Code notice for a large result,
so 62 of the 234 Claude Code runs counted a result the agent could not open as a
readable answer. The fix is in `score.py` with a regression test. Rescoring the
raw logs with it changed 62 rows and flipped the success of 25. The Codex logs
rescore to identical rows, so the rescoring itself is faithful. Calls, wrong
picks, first call, and tokens did not change; only "expected tool answered" did.

| Claude Code, expected tool answered | Before the fix | After the fix |
|---|---|---|
| `full`, all 26 tasks | 95% | 87% |
| `router`, all 26 tasks | 95% | 82% |
| `core_router`, all 26 tasks | 85% | 73% |
| `full`, without the service-story task | 99% | 91% |
| `router`, without the service-story task | 99% | 85% |
| `core_router`, without the service-story task | 88% | 76% |

Claude Code runs that hit a saved preview, by surface (78 runs each):

| Surface | Runs with at least one saved preview | Success flips |
|---|---|---|
| `full` | 26 | 6 |
| `router` | 20 | 10 |
| `core_router` | 16 | 9 |

This is a finding in its own right. A result the model cannot read is a failed
call on that client, whatever surface asked for it. It is the motivating case
for the oversized story response (#8798) and the one-copy result shape (#8800):
smaller results would have stayed inline. Codex logs show no such notice. Codex
bounds large output by a different mechanism that this harness did not measure,
so these counts say nothing about Codex.

The 25 flips cluster on six tasks whose tools return large lists on every
surface (code search, dead code, repository listing, one drift query). Of the
25, 10 were on `router`, 9 on `core_router`, and 6 on `full`. Claude Code's
result limit, not the surface, caused them. The harness raises
`MAX_MCP_OUTPUT_TOKENS`, which was not enough to keep these results inline.

### Headline with and without the service-story task

The service-story task (`service-workload-001`, expected tool
`get_service_story`) failed in all 18 runs, on every surface and both clients,
because of defects in that tool that are not deployed yet (see Known gaps).
It is one task of 26, so it lowers each row by three to four points. Dropping
the whole `service_workload` family, which also drops the unaffected
`investigate_service` task, moves the answered rate by at most one point.

All 26 tasks (78 runs per row):

| Client | Surface | Runs | Expected tool answered | First call correct | Genuine wrong-pick rate | Mean tool calls | Mean input tokens |
|---|---|---|---|---|---|---|---|
| claude | full | 78 | 87% | 95% | 40% | 3.81 | 330,584 |
| claude | router | 78 | 82% | 99% | 28% | 3.91 | 44,743 |
| claude | core_router | 78 | 73% | 73% | 49% | 4.45 | 85,966 |
| codex | full | 78 | 92% | 76% | 69% | 7.65 | 250,997 |
| codex | router | 78 | 90% | 87% | 54% | 11.01 | 202,653 |
| codex | core_router | 78 | 82% | 65% | 54% | 11.69 | 211,549 |

Without the service-story task (75 runs per row):

| Client | Surface | Runs | Expected tool answered | First call correct | Genuine wrong-pick rate | Mean tool calls | Mean input tokens |
|---|---|---|---|---|---|---|---|
| claude | full | 75 | 91% | 95% | 40% | 3.81 | 328,350 |
| claude | router | 75 | 85% | 99% | 26% | 3.73 | 43,505 |
| claude | core_router | 75 | 76% | 76% | 47% | 4.27 | 84,181 |
| codex | full | 75 | 96% | 75% | 69% | 7.68 | 248,507 |
| codex | router | 75 | 93% | 87% | 53% | 10.72 | 193,700 |
| codex | core_router | 75 | 85% | 68% | 52% | 11.44 | 203,938 |

Treat all of these as a proxy. In the 22-run manual check below, the scored
success matched my reading of the answer in 13 runs (59%).

What the numbers support:

- Claude Code: `full` has the best answered rate in this proxy and `router` is
  not tied with it (91% against 85% without the story task, a gap of about
  4 runs in 75). `router` had the better
  first call (99% against 95%) and used about 43,500 input tokens per run
  against about 328,000 for `full`.
- Codex: `full` answered slightly more often than `router` (96% against 93%).
  `router` used fewer input tokens (about 194,000 against 249,000) and needed
  more calls (10.7 against 7.7).
- `core_router` is the weakest surface for both clients on first call and on
  answers. The 10-tool list from #8759 covers part of what these questions
  need, so the agent often leaves it through `find_tool`.

What they do not support: a claim about answer quality beyond the scored proxy
and the 22-run check, a ranking of the clients against each other (they ran
different models under different harness rules), or a gap between `full` and
`router` smaller than a few runs. Each cell has 3 repeats of 26 tasks.

### Per task

One row per task. "ok" counts runs where the expected or an alternate tool
answered. "wrong" is the mean number of non-expected domain calls per run and
includes helper calls, so it is larger than the genuine wrong-pick rate.

<details>
<summary>Per-task table: 26 tasks, 6 client and surface columns</summary>

| Task | Expected tool | claude full | claude router | claude core_router | codex full | codex router | codex core_router |
|---|---|---|---|---|---|---|---|
| change-impact-001 | find_blast_radius | 3/3 ok, 11.3 calls, 5.7 wrong | 3/3 ok, 3.3 calls, 0.3 wrong | 1/3 ok, 7.3 calls, 6.3 wrong | 3/3 ok, 7.7 calls, 5.7 wrong | 3/3 ok, 8.7 calls, 2.3 wrong | 1/3 ok, 6.7 calls, 5.0 wrong |
| change-impact-004 | investigate_change_surface | 3/3 ok, 7.3 calls, 4.3 wrong | 3/3 ok, 8.7 calls, 3.3 wrong | 3/3 ok, 6.0 calls, 3.7 wrong | 3/3 ok, 22.7 calls, 20.3 wrong | 3/3 ok, 16.7 calls, 8.3 wrong | 3/3 ok, 14.3 calls, 10.3 wrong |
| code-graph-002 | analyze_code_relationships | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 2/3 ok, 2.7 calls, 1.3 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 0/3 ok, 2.0 calls, 2.0 wrong |
| code-graph-005 | get_code_relationship_story | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 4.0 calls, 0.7 wrong | 3/3 ok, 4.3 calls, 0.0 wrong | 3/3 ok, 3.0 calls, 0.0 wrong |
| code-lookup-001 | find_symbol | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.7 calls, 0.0 wrong | 0/3 ok, 2.0 calls, 2.0 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 0/3 ok, 2.0 calls, 2.0 wrong |
| code-lookup-005 | find_code | 0/3 ok, 2.0 calls, 1.0 wrong | 0/3 ok, 3.0 calls, 1.0 wrong | 0/3 ok, 2.0 calls, 1.0 wrong | 3/3 ok, 2.0 calls, 0.7 wrong | 3/3 ok, 4.3 calls, 0.7 wrong | 3/3 ok, 3.0 calls, 1.0 wrong |
| dead-code-001 | find_dead_code | 3/3 ok, 3.0 calls, 1.0 wrong | 3/3 ok, 5.3 calls, 1.0 wrong | 1/3 ok, 4.3 calls, 1.3 wrong | 0/3 ok, 1.0 calls, 1.0 wrong | 3/3 ok, 2.7 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 1.0 wrong |
| dead-code-005 | find_cross_repo_dead_code | 2/3 ok, 8.7 calls, 4.0 wrong | 0/3 ok, 5.7 calls, 0.7 wrong | 0/3 ok, 4.7 calls, 1.3 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 3.7 calls, 0.0 wrong | 3/3 ok, 3.0 calls, 0.0 wrong |
| documentation-001 | list_documentation_findings | 3/3 ok, 5.3 calls, 4.3 wrong | 3/3 ok, 3.7 calls, 0.7 wrong | 3/3 ok, 4.3 calls, 1.3 wrong | 3/3 ok, 42.7 calls, 40.7 wrong | 3/3 ok, 18.7 calls, 12.0 wrong | 3/3 ok, 77.3 calls, 52.0 wrong |
| documentation-005 | count_documentation_findings | 3/3 ok, 1.7 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong |
| drift-replatforming-001 | find_unmanaged_resources | 3/3 ok, 6.0 calls, 3.3 wrong | 0/3 ok, 8.3 calls, 3.7 wrong | 2/3 ok, 7.0 calls, 4.3 wrong | 3/3 ok, 11.7 calls, 10.3 wrong | 3/3 ok, 23.0 calls, 6.3 wrong | 3/3 ok, 29.7 calls, 6.3 wrong |
| drift-replatforming-002 | get_iac_management_status | 3/3 ok, 1.3 calls, 0.3 wrong | 3/3 ok, 3.0 calls, 1.0 wrong | 3/3 ok, 5.0 calls, 2.0 wrong | 3/3 ok, 2.0 calls, 1.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 3.3 calls, 0.3 wrong |
| infrastructure-001 | find_infra_resources | 3/3 ok, 6.0 calls, 4.3 wrong | 3/3 ok, 4.0 calls, 1.0 wrong | 3/3 ok, 7.7 calls, 4.3 wrong | 3/3 ok, 21.3 calls, 18.0 wrong | 3/3 ok, 21.0 calls, 14.0 wrong | 3/3 ok, 15.7 calls, 11.7 wrong |
| infrastructure-004 | investigate_resource | 3/3 ok, 4.0 calls, 3.0 wrong | 3/3 ok, 3.0 calls, 1.0 wrong | 3/3 ok, 6.0 calls, 4.0 wrong | 3/3 ok, 13.0 calls, 11.3 wrong | 3/3 ok, 23.7 calls, 19.0 wrong | 3/3 ok, 12.0 calls, 9.7 wrong |
| investigation-001 | list_query_playbooks | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 3.3 calls, 0.0 wrong | 3/3 ok, 2.3 calls, 0.0 wrong |
| investigation-003 | list_investigation_workflows | 3/3 ok, 3.0 calls, 1.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 2/3 ok, 3.3 calls, 0.7 wrong | 3/3 ok, 2.7 calls, 1.3 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.7 calls, 0.7 wrong |
| operations-001 | get_index_status | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong |
| operations-002 | get_hosted_readiness | 3/3 ok, 2.0 calls, 1.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong |
| repository-overview-001 | list_indexed_repositories | 0/3 ok, 4.7 calls, 3.0 wrong | 1/3 ok, 7.3 calls, 1.0 wrong | 1/3 ok, 11.0 calls, 6.0 wrong | 3/3 ok, 8.0 calls, 0.0 wrong | 3/3 ok, 11.7 calls, 0.0 wrong | 3/3 ok, 12.7 calls, 0.0 wrong |
| repository-overview-004 | count_repositories_by_language | 3/3 ok, 1.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 1.0 calls, 0.0 wrong | 0/3 ok, 2.0 calls, 1.0 wrong | 0/3 ok, 2.3 calls, 1.3 wrong |
| security-posture-002 | list_secrets_iam_privilege_posture_observations | 3/3 ok, 9.7 calls, 7.3 wrong | 3/3 ok, 5.3 calls, 2.0 wrong | 3/3 ok, 7.0 calls, 3.0 wrong | 3/3 ok, 18.0 calls, 17.0 wrong | 1/3 ok, 71.3 calls, 35.7 wrong | 3/3 ok, 45.0 calls, 10.3 wrong |
| security-posture-004 | list_secrets_iam_posture_gaps | 3/3 ok, 2.0 calls, 1.0 wrong | 3/3 ok, 3.0 calls, 1.0 wrong | 3/3 ok, 3.0 calls, 1.0 wrong | 3/3 ok, 12.3 calls, 11.3 wrong | 3/3 ok, 24.3 calls, 11.0 wrong | 3/3 ok, 20.0 calls, 8.3 wrong |
| service-workload-001 | get_service_story | 0/3 ok, 3.7 calls, 2.7 wrong | 0/3 ok, 8.3 calls, 4.0 wrong | 0/3 ok, 9.0 calls, 6.0 wrong | 0/3 ok, 7.0 calls, 6.0 wrong | 0/3 ok, 18.3 calls, 10.0 wrong | 0/3 ok, 18.0 calls, 14.3 wrong |
| service-workload-004 | investigate_service | 3/3 ok, 5.0 calls, 2.0 wrong | 3/3 ok, 5.3 calls, 1.3 wrong | 3/3 ok, 5.0 calls, 0.3 wrong | 3/3 ok, 11.0 calls, 7.0 wrong | 3/3 ok, 8.0 calls, 2.3 wrong | 3/3 ok, 13.7 calls, 9.7 wrong |
| supply-chain-003 | count_supply_chain_impact_findings | 3/3 ok, 2.0 calls, 0.0 wrong | 3/3 ok, 2.7 calls, 0.0 wrong | 3/3 ok, 2.3 calls, 0.0 wrong | 3/3 ok, 1.7 calls, 0.7 wrong | 3/3 ok, 3.7 calls, 0.3 wrong | 3/3 ok, 4.3 calls, 1.0 wrong |
| supply-chain-004 | get_supply_chain_impact_inventory | 3/3 ok, 3.3 calls, 0.3 wrong | 3/3 ok, 3.0 calls, 0.0 wrong | 3/3 ok, 4.0 calls, 0.0 wrong | 3/3 ok, 1.3 calls, 0.0 wrong | 3/3 ok, 3.0 calls, 0.0 wrong | 3/3 ok, 3.0 calls, 0.0 wrong |

</details>

## Manual check of the scoring

I read the raw event logs of 22 runs and judged each final answer against the
tool results in the same log: Correct (the answer follows from the results and
answers the question), Partly (it follows from the results but misses part of
the question or says so), Wrong, or No data (the tools returned nothing to
answer from). The sample is chosen, not random: 12 scored successes and 10
scored failures, spread over both clients, all three surfaces, and the
service-story task. Corpus ids and family labels only; no entity names.

"Proxy vs judgment" is `agree` when a scored success has a Correct or Partly
answer or a scored failure has a Partly, Wrong, or No data answer. It is `too
generous` when a success has a No data or Wrong answer, and `too strict` when a
failure has a Correct answer.

<details>
<summary>Sample table: 22 runs, one row each</summary>

| Corpus id | Client | Surface | Repeat | Scored | Judgment | Proxy vs judgment | Reason |
|---|---|---|---|---|---|---|---|
| operations-001 | claude | router | 0 | ok | Correct | agree | Status summary matches the status payload. |
| code-graph-002 | claude | router | 0 | ok | Correct | agree | Callee list matches the relationship story result. |
| code-graph-005 | claude | full | 0 | ok | Correct | agree | Caller and callee table matches the story result. |
| code-lookup-001 | claude | core_router | 0 | failed | Partly | agree | Found the file through content search; the line number was approximate and off. |
| documentation-001 | claude | full | 1 | ok | No data | too generous | Zero findings in the whole index; the agent said so honestly. |
| service-workload-001 | claude | router | 1 | failed | Partly | agree | Story call over the byte budget; answer built from the deployment tool, with caveats. |
| service-workload-001 | claude | full | 2 | failed | Partly | agree | Story call over the byte budget; answer from the service investigation, marked partial. |
| change-impact-001 | claude | core_router | 1 | ok | No data | too generous | Blast radius returned empty and the agent hedged. |
| drift-replatforming-001 | claude | router | 0 | failed | Partly | agree | Findings call over the byte budget; the agent said it could not confirm the account. |
| supply-chain-003 | claude | router | 0 | ok | No data | too generous | Count of zero for the service; cannot tell a clean service from missing scanner data. |
| repository-overview-001 | claude | full | 0 | failed | Partly | agree | Repository list over the byte budget; five repositories found by code search. |
| code-lookup-001 | codex | router | 0 | ok | Correct | agree | Definition lookup returned one result and the answer matches it. |
| repository-overview-004 | codex | full | 0 | ok | Correct | agree | Count tool returned the figure the answer states. |
| operations-002 | codex | core_router | 0 | ok | Correct | agree | Readiness verdict and counts match the report. |
| dead-code-001 | codex | full | 0 | failed | Correct | too strict | Used the relationship tool's dead-code query, which is a valid route; the corpus does not list it as an alternate. |
| repository-overview-004 | codex | router | 1 | failed | Correct | too strict | Used the by-language list tool; its untruncated result held the count the answer states. |
| code-graph-002 | codex | core_router | 0 | failed | Correct | too strict | Read the file content through two tools; the callee description matches the code. |
| supply-chain-004 | codex | full | 1 | ok | No data | too generous | Zero findings for the environment, so nothing to group by severity. |
| service-workload-001 | codex | router | 0 | failed | Partly | agree | Story call over the byte budget; answer from context and deployment tools, labeled configuration-only. |
| documentation-001 | codex | core_router | 0 | ok | No data | too generous | No findings; the agent reported an evidence gap. |
| security-posture-002 | codex | router | 0 | ok | No data | too generous | 171 calls, then an honest statement that no roles could be identified. |
| security-posture-002 | codex | router | 2 | failed | No data | agree | The agent could not verify any role and said so. |

</details>

Result: 13 of 22 agree (59%), 6 are too generous (27%), and 3 are too strict
(14%).

- All six `too generous` runs are successes where the tools returned nothing.
  The agents reported the gap honestly, so none of them invented an answer, but
  the proxy still counts them as answered. A zero count may be a true zero or
  missing data. I treated an empty result as No data throughout.
- All three `too strict` runs are Codex runs where a sibling tool returned a
  complete, correct result. The genuine wrong-pick rate counts those picks as
  wrong, so it overstates how often Codex fails to reach an answer.
- The three service-story runs I read each ended with a partial answer built
  from other tools, and each said the story call was too large.
- I found no Wrong answer. Every answer I read followed from its tool results.

## What changed since the pilots

The earlier pilots ran one repeat per cell and used the old `core_router` list.
This matrix uses the refreshed 10-tool list from #8759 and three repeats, and
all runs hit one deployed build. The `core_router` rows here are therefore not
comparable with the pilot numbers.

## Known gaps

- The service-story task fails on every surface for both clients. The causes are
  `get_service_story` defects: an ambiguous-service error that advises arguments
  the tool rejects (#8797), a response over the 262,144-byte budget (about
  560 KB for the service tested, #8798), name-only workload nodes left over
  from an identity change (#8795), and a result shape with no one-copy form
  (#8800). None of the fixes is deployed to this build.
- The `core_router` list was chosen from one QA window with a dispatch-entry
  bias. #8799 tracks sizing that bias and refreshing the list.
- Six tasks return results larger than Claude Code will inline. They cost
  Claude Code 25 answers on every surface until those tools return smaller
  results or the client limit changes.

## Caveats

- One model per client. A different model may pick tools differently.
- "Expected tool answered" is a proxy; the manual check shows it is about
  three in five agreement with a human reading on a chosen sample, not a
  random one.
- 3 repeats of 26 tasks is a thin sample. Differences of a few runs may be noise.
- One QA deployment and one build. Production traffic and data volume differ.
- The corpus alternates list some valid routes (such as the relationship tool's
  dead-code query) unevenly. That can make the genuine wrong-pick rate too high.

## NOT_CHECKED

- Answer correctness beyond the 22-run sample.
- Other clients and models, including Cursor and VS Code.
- Production traffic, or any deployment other than the one QA build.
- The service-story task after the #8795, #8797, #8798, and #8800 fixes land.
- Whether a larger Claude Code result limit changes the `router` and `full`
  answered gap.
- Codex cost, which the client does not report.
