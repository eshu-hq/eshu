# 8874 cutover-readiness re-pin

Refs #8874. Part of #6707. Status: evidence record only. This file
re-verifies the population-complete census against current
`origin/main` and maps every acceptance bullet of #8874 to its
evidence, so the cutover decision rests on current truth instead of
the census base.

## Step-4 inventory verdict: no repository creation needed

The lane brief premise ("destination repo eshu-cli does not exist
yet") is stale. Organization inventory was verified live with
`gh repo list eshu-hq`:

- `eshu-hq/eshu-cli` exists, created 2026-10-28.
- Main head `820daff7ba9057e34df50ca4f2574f2ca3491a39`, unchanged
  since the population-complete census cited it.
- Tags `v0.1.0` (`6dc39b0679fb3b0bbedd4569c8d69fdb39e9a2b6`) and
  `v0.1.1` (`887c52cbfc9e39eb9eb5f12e4ec3f6eab5b8e021`); releases
  `v0.1.0` (2026-10-31) and `v0.1.1` (2026-11-01) cut.
- The issue body still says "current organization inventory contains
  no destination repository" and pins inventory main `50e55647`;
  both statements are superseded. Updating the issue body is a
  coordinator/owner call, not done in this lane.

Related inventory notes: the epic link is #6707 (#8879 is the
sibling ingester satellite, not the epic); the named prior worktrees
`/home/ubuntu/personal-repos/eshu-8879-caller` (absent; the live
name is `eshu-8879-caller-lane`) and `eshu-8879-remainder` touch
document/media import and ingester rollback readiness only and were
inspected read-only.

## Re-verification at current base

Base: `origin/main` `35023cc39d8db227cb9d6ebe6ef515f2e06cf171`.
Prior census base: `0650a6a4d2`. Method: the exact closure census
from `8874-cli-population-complete.md` (`go list -deps` counts of
`eshu-hq/eshu/go/internal` packages, `git grep` consumer shape,
`openapi/paths/cli` listing), plus the in-tree guard tests.

Closure counts, recorded versus re-verified (identical, zero drift):

| Family | Recorded | Re-verified |
| --- | --- | --- |
| `investigation` | 463 | 463 |
| `scan` | 3 | 3 |
| `vulnscan` | 503 | 503 |
| `docs` | 463 | 463 |
| `demo` | 9 | 9 |
| `firstrun` | 7 | 7 |
| `firstrunbench` | 8 | 8 |
| `hosted` | 511 | 511 |
| `assistantguidance` | 507 | 507 |
| `compparity` | 472 | 472 |
| `evbundle` | 15 | 15 |
| `evidpacket` | 2 | 2 |
| `packetdogfood` | 1 | 1 |
| `graphinstall` | 463 | 463 |
| `admin` | 463 | 463 |
| `hookpreflight` (shared-owner note) | 1 | 1 |

Blocking-shape spot checks, re-verified:

- `go list -deps ./internal/cli/scan` still resolves `procexec`,
  `eshulocal`, `scan` only under `eshu-hq`.
- `go/internal/query/openapi/paths/cli/` still holds
  `capture compose render score validate` only (plus package docs);
  no packet-build endpoint, so `investigation export` stays stubbed.
- Guard tests green on this base: `go test ./cmd/eshu/ -run
  'TestExtractionInventory|TestNoMcpsetup|TestScanNonTestImports'`
  (`ok`, exit 0). Inventory still covers every visible top-level
  command; `admin` still RETAIN; the S1 `evidredact->mcpsetup` edge
  still cut.

Population chain on `main` (all merged, none re-done here):
`#9504` open-questions verdict, `#9512` S1 core seams plus
population-scope verdict, `#9529` S2 SDK wire-contract module,
`#9530` S4 core endpoints, `eshu-cli#22` contract re-seed,
`eshu-cli#23` Tier-1 population, `eshu-cli#25` Tier-2 repoint,
`eshu-cli#26` Tier-3 doctor plus workspace, `#10080`
population-complete census.

## Acceptance mapping

- Inventory commands, flags, env, output/exit contracts; core-only
  versus user split: DONE in-tree (`go/cmd/eshu/
  cli_extraction_inventory.md`, 58 rows; `8874-eshu-cli-contract.md`;
  guard tests green above).
- Supported server/client pairs, authed API/MCP behavior,
  install/upgrade workflows: PARTIAL. Tier-1 parity 93/93 and
  releases cut; deployed-path proof against QA: NOT_CHECKED.
- Named implementation, public-contract, release and deployment
  owners; production target and configuration source: DONE
  (owner-confirmed `@linuxdynasty`; owner-designated QA EKS
  environment; rollback-path proof recorded 2026-10-28).
- Current packages, fact kinds and consumers, durable-state
  ownership, protocols, artifacts, migration stage, prerequisites:
  DONE (`8874-cli-population-complete.md` plus this re-pin).
- Behavior and public-contract parity on representative and hostile
  inputs: PARTIAL. Populated families carry per-PR parity and
  review; remaining stubs are decided-retain, extension-host-bound,
  or shared-owner-blocked, so no parity is owed for them; deployed
  parity: NOT_CHECKED.
- Extracted deployed path preserves reducer, canonical graph, HTTP
  API and MCP truth: NOT_CHECKED (needs the QA window).
- Concurrent retry, duplicate delivery, ordering, cancellation,
  revoked authority, stale claims, partial/ambiguous failures:
  NOT_CHECKED (needs the deployed path).
- Bounded resource use, no regression, health/readiness/freshness,
  metrics, traces, structured logs, status: NOT_CHECKED (needs the
  deployed path).
- Credential handling, artifact provenance, vulnerability response,
  oldest/newest version pairs: PARTIAL. Compatibility matrix and
  credential surfaces live in the contract record; release
  attestation rides the release workflow; deployed verification:
  NOT_CHECKED.
- Independent build, release and deploy on public contracts:
  PARTIAL. Independent build and release proven (`v0.1.0`,
  `v0.1.1`; exact pins, no replace/work, no fork); deployment and
  consumption proof: NOT_CHECKED.
- Rollback rehearsal: DOCUMENTED ONLY (restore procedure in the
  2026-10-28 rollback-path proof); execution: NOT_CHECKED (needs a
  cutover candidate and the QA window).
- Independent review, local proof, stable exact-head CI; removal
  after production acceptance: PARTIAL. Per-PR review and CI done;
  in-tree removal: NOT STARTED (gated on production acceptance).

## Exact remaining steps to cutover

1. Deployed-path parity of the destination releases against QA EKS
   (needs owner QA window; closes the NOT_CHECKED bullets above
   except rollback and removal).
2. Executed rollback rehearsal against a cutover candidate (needs
   the QA window; the documented procedure is the plan).
3. Owner production-acceptance statement (gate for removal).
4. In-tree removal under a removal plan (gated on step 3).

## NOT_CHECKED in this lane

- Deployed-path parity of the destination releases against QA.
- Rollback execution (documented only, unchanged).
- Issue-body refresh (coordinator/owner call).
- In-tree removal (gated on production acceptance).
