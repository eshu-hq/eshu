# OSV Alpine queries carry the release (eshu-hq/eshu#9993)

**Refs #9993.** The installed-evidence derivation sent Alpine rows to OSV under
the bare ecosystem name `Alpine`. OSV matches Alpine records per release, so the
bare name returned no advisories and no Alpine package ever got a finding.

## What changed

- The coordinator reads the release from the installed row's `distro_version`
  (`3.18.4` becomes `v3.18`) and groups Alpine queries by it.
- The derived scope id gains a `release` parameter:
  `vuln-intel://osv/alpine/curl?release=v3.18&version=7.87.0-r0`, and
  `vuln-intel://osv/alpine/_batch?query=...&release=v3.18` for a batch. Two
  releases of one package at one version are two scopes, and neither supersedes
  the other.
- The collector half ships in the satellite repo
  `eshu-collector-vulnerability-intelligence` (pull request
  eshu-hq/eshu-collector-vulnerability-intelligence#52, merged, and its README
  wording follow-up #53 in the same repo), not in this tree. The satellite rebuilds its queries from the scope id alone,
  so it reads the `release` parameter (once, `alpine` only, `vMAJOR.MINOR` only;
  anything else is refused) and sends `Alpine:v3.18`. The target's own ecosystem
  stays `alpine`. The in-tree collector (`go/internal/collector/vulnerabilityintelligence`)
  is not changed: it ignores the parameter and still queries the bare name, so
  this fix takes effect only where the satellite runs the claim.
- Contract: the planner emits the scope-id query parameter `release` with value
  `v3.18`; the two literals pinned on both sides are
  `vuln-intel://osv/alpine/curl?release=v3.18&version=7.87.0-r0` and
  `vuln-intel://osv/alpine/_batch?query=curl%407.87.0-r0&query=zlib%401.2.13-r1&release=v3.18`.
- A row whose `distro_version` names no `major.minor` line (blank, `edge`, `3`)
  is skipped with `derived_target_missing_release` skip evidence. It is never
  sent as a bare `Alpine` query.

No fact payload or `sdk/go/factschema` field changed: `distro_version` was
already on the `vulnerability.os_package` fact and already selected by the
installed-target reader.

## Measurements (public OSV API, 2026-11-04)

`POST /v1/querybatch`, one query, only the ecosystem string changed:

| Ecosystem sent | Package and version | Advisories |
|---|---|---|
| `Alpine` | `curl` `7.87.0-r0` | 0 |
| `Alpine:v3.18` | `curl` `7.87.0-r0` | 31 |
| `Alpine:v3.17` | `curl` `7.87.0-r0` | 24 |
| `Debian` | `curl` `7.74.0-1.3+deb11u1` | 143 |
| `Debian:11` | `curl` `7.74.0-1.3+deb11u1` | 8 |

The recorded Alpine answers (trimmed to three ids) live with the satellite's
tests, in the OSV test data directory of the satellite repository.

## Debian, Ubuntu, Rocky, Alma: not changed

Decision: these stay on their bare ecosystem names and their existing scope ids.

Bare `Debian` answers across all releases (143 against 8), so it over-fetches by
about 18 times for this package but loses nothing: the reducer's release-line
selection (#9806, `osHostReleaseLine`) narrows the ranges to the installed
row's line after the fetch. Alpine is different in kind: the bare name returns
nothing, which is a correctness failure. Debian's bare answer is an efficiency
question, so it stays out of this change. Qualifying Debian and Ubuntu would
move every existing derived scope id and orphan the current source states.
The over-fetch was measured on one package only (curl). Revisit the decision
when a fleet-wide measurement shows the Debian or Ubuntu over-fetch is a real
cost.

NOT_CHECKED: the Ubuntu, Rocky, and Alma answers from OSV, and whether the
provider should send a PURL with the query.

## Cost

One string build per query in the coordinator. No new
SQL, no new metric label. The scope id grows by 14 bytes per release-qualified
chunk; the chunk sizer counts them, so no chunk exceeds the 1800-byte cap.
