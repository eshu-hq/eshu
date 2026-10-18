# Item 9: single-repo flag-on arm — hold theory DISPROVEN at this scale (Refs #7638)

Status: **measured GREEN (consumer converges); the expected hold did NOT
reproduce.** No production code changed in this step. Only this file is new.
The runbook in `7638-obligation-proof-item1-blocked.md` predicted
hold-until-later-commit (`no_memo_baseline` refusal at lease cadence) for a
single-repo quiet generation with the flag on. That prediction is wrong on
built binaries at this scale: the obligation completed in ~11 s.

## Run setup (verified by the coordinator, not only the drive agent)

- Separate stack, zero contact with the item-1 drive: compose project
  `7638item9`, fixture `/home/ubuntu/pcg-remote-runs/7638item9-drive/`
  (single repo `solo-app`: README, app.txt, Flux `GitRepository/solo-app`
  with synthetic `ESHU_GITHUB_ORG=item9drive` RemoteURL), disjoint host
  ports (neo4j 27474/27687, postgres 25432, api 28080, mcp 28081, others
  29564-29571), source reused read-only at `1664cf2f`.
- Flag verified on for this arm: `docker inspect` on the resolution-engine
  container reads `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=true`
  (and the consumer completed work — see below).
- Isolation verified: all 7 `7638item1-*` containers stayed Up with
  undisturbed uptimes (30/41 min) throughout; the item-1 fixture gained no
  files; the item-1 control obligation was still `pending` after this arm.
- Operator-local env file stays on the run host, off-repo (same rule as
  item 1: no host identity, key paths, or ports in public text beyond the
  port numbers above, which carry no credential).

## What happened (UTC 2026-10-18, coordinator-verified)

| Event | Time | Evidence |
|---|---|---|
| Baseline gen `051909ee38a47a3229c593ffceee8c29e757f16120d21b70346e8b413e88d33e` active | 12:48:32.146 | `scope_generations.observed_at`, status now `superseded` |
| Quiet mutation (host-only README append) | 12:50:01 | single mutation, no fix attempted |
| New gen `405582d7fa667f33f474b8cac15485043c42826246d44a7341f065852f922448` observed | 12:50:01.978 | `<1` s after the mutation |
| Fence then complete | 12:50:12.487 → 12:50:12.532 | `activation obligation finalized … outcome=phase_not_ready woken=0` then `outcome=completed woken=1 claim_token=1` (engine log, verbatim fields) |
| Obligation `completed` | — | `activation_obligations` row for `405582d7fa667f33f474b8cac15485043c42826246d44a7341f065852f922448` is `completed`; 2 completed / 0 pending overall |

Mutation to completed in ~11 s — the same fence-then-complete shape as the
two-repo flag-on arm, not a hold. `rg -i no_memo_baseline` over all item-9
service logs: zero lines. `refusal`: zero lines. `fact_work_items` ended
33/33 succeeded with nothing pending or retrying. No new generations after
12:50:01.

## Consequence for the default flip

The single-repo hold is not a blocker at this scale: with one admitted
candidate the targeted-maintenance baseline (widened by #7887) admits it and
the consumer settles the obligation inside one lease. The item-5
predicate-pushdown question (corpus-proportional cost) is unaffected — this
arm says nothing about 984 scopes, and the item-3 p99 margin analysis
stands. If a hold exists, it needs a larger or adversarial corpus to show;
do not re-run single-repo holds at this scale as flip evidence.

## Caveats

1. Timestamps and log lines above were re-read by the coordinator through
   `docker exec … psql` and `docker logs`; the stack build, port picks, and
   11.5-minute observation window are drive-agent-reported and were
   sanity-checked (container uptimes, obligation states) but not
   line-by-line replayed.
2. The item-9 stack is left keep-live for further arms; retention mode is
   stop-and-preserve until the flip decision lands, then destroy with the
   item-1 stack. Teardown must name project `7638item9` exactly — never
   broad-prune.
