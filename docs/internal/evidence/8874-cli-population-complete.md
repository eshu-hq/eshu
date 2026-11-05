# 8874 population-complete census

Refs #8874, part of #6707. Status: evidence record only. This file
proves that no stubbed family in `eshu-hq/eshu-cli` main can populate
without violating the population-scope verdict or the S4 decisions.

Base: `origin/main` `0650a6a4d2`. Destination: `eshu-hq/eshu-cli` main
`820daff7b` (18 stubs in `cmd/eshu/stubs.go`; releases `v0.1.0`,
`v0.1.1` cut). Method: `go list -deps` closure census (decisive route)
plus `git grep` consumer census, both run in the worktree at the base
commit. Prior decisions:
`8874-cli-population-scope-verdict.md` (Tier 1 + E3/E4/E5, Tier 2 gated
on S2, Tier 3 gated on S4) and `8874-s4-endpoint-decisions.md`.

## Already populated in the destination (not re-censused here)

Tier-1 families, Tier-2 repoint (`eshu-cli#25`: report, service-report,
scorecard, component-thin, config, mcp-setup), Tier-3 doctor +
workspace (`eshu-cli#26`), install, ecosystem overview.

## Stub-family census

Counts are `eshu/go/internal` packages in `go list -deps`. Consumers
are non-test, non-doc production files.

| Family | Closure | Blocking dependency / consumer | Disposition |
| --- | --- | --- | --- |
| `investigation` | `cli/investigation` 463 (via `query`) | Packet build lives in `cli/investigation/fetch.go`; `openapi/paths/cli/` holds capture/compose/render/score/validate only, no packet-build endpoint | Stays stubbed; render endpoint alone cannot serve export |
| `scan` | `cli/scan` 3, one of which is `eshulocal` | Local-machine root resolution; local-owner modes need `local-host/v1` | Retain-forever per S4 |
| `vuln-scan` | `cli/vulnscan` 503 | `vulnerabilityparity` shared with `cli/vulnscan/providerparity.go` and `internal/vulnerabilityparityproof/*` | Retain-forever per S4 |
| `docs` | `cli/docs` 463 | `doctruth` + Postgres persistence + local file reads | Retain-forever per S4 |
| `demo`, `demo-benchmark` | `cli/demo` 9 incl. `eshulocal`, `cli/scan`, `cli/firstrun` | Local demo lifecycle over local-owner scan | Retain |
| `first-run`, `first-run-benchmark` | `cli/firstrun` 7, `cli/firstrunbench` 8, incl. `eshulocal`, `cli/scan` | Local onboarding over local-owner scan | Retain |
| `hosted-setup`, `hosted-onboard` | `cli/hosted` 511 (via `mcp`) | First-connection flow over MCP + scan | Stays stubbed |
| `assistant` | `cli/assistantguidance` 507 (via `mcp`) | `cli/hookpreflight` is pure (closure 1) but shared with core `internal/cigates/docsset.go` | Shared-owner-blocked; guidance stays on `mcp` closure |
| `competitive-parity` | `cli/compparity` 472 | `internal/competitiveparity` shared with `cli/compparity/inventory.go`; a move drags `query`/`capabilitycatalog` | Shared-owner-blocked, closure-blocked |
| `evidence` | `cli/evbundle` 15 (status/*, `evidencebundle`, `redact`); `cli/evidpacket` 2 | `internal/evidencebundle` and `cli/evbundle` shared with core `internal/query/evidence/bundle_live.go`; `cli/evidpacket` shared with `cli/compparity/exercises.go` | Shared-owner-blocked on two roots |
| `evidence-packet-dogfood` | `internal/packetdogfood` closure 1 (pure) | Consumers: `cmd/eshu/evidence.go`, `cli/evidpacket/dogfood.go`, `cli/compparity/exercises.go`, core `internal/cigates/docsset.go` | Shared-owner-blocked despite pure closure |
| `graph` | `cli/graphinstall` 463 | `localsupervisor` + `eshulocal` sidecar lifecycle | Retain-forever per S4 |
| `watch`, `w` | `eshulocal` direct | Watcher lifecycle is local execution | Retain-forever per S4 |
| `admin` | `cli/admin` 463 + `storage/postgres` | Core-owned schema/recovery state; migration plan keeps bootstrap/repair utilities in `eshu` | Core-only utility, retain-forever |
| `service` local profiles | `cli/servicereport` 463 et al | Offline compose already a thin client in dest via S4 compose endpoint; local-owner start/stop stays | Split complete: thin part populated, local part retained |
| `component` local verbs | `cli/component` 20 | Conform runs bounded extension execution in-process; grants use local trust-state files | Extension-host-bound per S4; inventory/diagnostics thin in dest |

## Exact census commands (run at the base commit)

```sh
git rev-parse HEAD # 0650a6a4d2
cd go
for p in <pkg> (...16 packages...); do
  go list -deps ./$p | rg -c 'eshu-hq/eshu/go/internal'
done
go list -deps ./internal/cli/scan | rg 'eshu-hq'       # procexec, eshulocal, scan
go list -deps ./internal/cli/demo | rg 'eshu-hq'       # 9 incl. eshulocal, scan, firstrun
go list -deps ./internal/cli/evbundle | rg 'eshu-hq'   # 15 incl. evidencebundle, status/*
git grep -l '<pkg>' -- go ':!*_test.go'                # per-package consumer census
ls go/internal/query/openapi/paths/cli/                # capture compose render score validate only
```

## Conclusion

The population is complete: every family that can populate under the
verdict rules has populated, and every remaining stub names its
decided disposition above. The unproven remainder of #8874 is not
population work:

1. Production acceptance (deployed-path parity + owner statement).
2. Executed rollback rehearsal (documented only; needs a cutover
   candidate and the owner-designated QA window).
3. In-tree removal (gated on production acceptance per the issue).

## NOT_CHECKED in this lane

- Deployed-path parity of the dest releases against QA.
- `local-host/v1` design for local-owner modes (named future route).
- `ScoreReport` endpoint (named S4 follow-up).
