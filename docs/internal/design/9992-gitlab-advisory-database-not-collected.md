# Do not collect the GitLab Advisory Database (#9992)

Status: Accepted (owner decision on #9992, 2026-11-05). This record approves
no collection, no schema change, and no deployment.

**Do not wire a live GitLab Advisory Database source: its terms forbid
storing its content, so the GLAD path stays fixture-only behind a guard test
until the owner and legal name a basis such as written consent from GitLab.**

## Context

The GitLab Advisory Database Terms (last updated May 2, 2022) state in
section 4(f) that a user agrees not to download, copy, or otherwise store
any of the content; section 5(f) bars automated access by spiders, robots,
crawlers, or data mining tools; section 5(b) bars using or mirroring the
database without written consent. Eshu's `vulnerability.*` facts store
advisory content in Postgres and the source cache writes it to disk, so a
live GLAD source would breach the terms on every sync. The exposure is
latent: the `NewGitLabSourceSnapshotEnvelope` and `GitLabAdvisoryEnvelopes`
constructors and a reducer `glad` source class exist, but no runtime provider
in `vulnruntime/provider.go` calls them. The owner and legal ruling (recorded
on #9992, 2026-11-05) is that Eshu does not collect GLAD.

## Options

| Option | Cost | Verdict |
|---|---|---|
| Collect GLAD with written consent from GitLab | Legal process plus provider work; ongoing terms risk | Rejected: no consent exists |
| Collect GLAD without consent | Breach of sections 4(f), 5(b), 5(f) | Rejected |
| Keep the normalizer fixture-only behind a guard test | Guard maintenance | **Chosen** |

## Decision

Scope: the GLAD normalizer, the `glad` source class, and all GLAD fixtures
stay. No runtime provider may reference the GitLab constructors. The guard
test `TestGitLabGateDetectsEveryKnownBypass` (plus `TestGitLabGateSeesTheWholeTree`
and `TestNoCodeProducesGitLabAdvisoryFacts` in
`go/internal/collector/vulnerabilityintelligence/vulnruntime/gitlab_gate_test.go`)
fails when any non-test file in `vulnruntime` refers to either constructor,
and proves its own scan on seeded sources. Non-goals: removing the normalizer,
changing the schema, or touching any other source.

## Consequences

Good:

1. No terms exposure from collection (#9992 tracks the remaining docs work).
2. The guard fails closed on new bypass shapes, including reflection by string.

Bad:

1. GLAD coverage stays absent; OSV, NVD, and GHSA carry that signal.
2. Any future GLAD use needs a new decision record citing the legal basis.

## Implementation plan

Affected paths: `go/internal/collector/vulnerabilityintelligence/vulnruntime/gitlab_gate_test.go`
(the gate), `go/internal/collector/vulnerabilityintelligence/README.md` and
`AGENTS.md` (fixture-only statements). Avoid: referencing the constructors
from runtime code, or weakening the gate to named spellings. Operator signal:
a red `TestGitLabGate*` test names the offending file and reference.

## Acceptance criteria

- [ ] This record exists under `docs/internal/design/` and states the ruling.
- [ ] `go test ./internal/collector/vulnerabilityintelligence/vulnruntime/ -run 'GitLab' -count=1` passes on a clean tree.
- [ ] A planted non-test reference to either constructor fails the gate.
- [ ] The collector README and `AGENTS.md` state the GitLab path is
      fixture-only and why.

## Decisions for the arbiter

None open. If consent from GitLab ever arrives, supersede this record rather
than editing it.

<details>
<summary>Evidence and measurements</summary>

- Terms text: `https://gitlab.com/gitlab-org/security-products/gemnasium-db/-/raw/master/LICENSE.md`, read 2026-11-04 (quoted on #9992).
- Owner ruling: comments on eshu-hq/eshu#9992, 2026-11-05 (GLAD not collected; guard stays).
- Gate delivery: PR #10127 (squash `df0fcdc0e9`); docs correction: PR #10125; phases banners: PR #10126.
- Source-terms table leaves `glad` out: `go/internal/collector/vulnerabilityintelligence/source_terms.go` (#9958).

</details>
