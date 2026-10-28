# Satellite Collector Cutover Runbook

Refs #9378. Part of #6707.

Read this before you open any satellite collector PR. It codifies the
machinery proven by the aws-cloud (#8852), cicd-run (#8854), grafana (#8857),
tempo (#8867), and terraform-state (#8868) cutovers so the remaining runtime
extractions (#8875-#8879 and the open collector lanes) reuse it instead of
rediscovering it. Reuse; do not re-prove a first transition.

## 0. How to use this runbook

Scope: moving one in-tree producer (`go/cmd/collector-<name>/`) to its own
`eshu-collector-<name>` satellite repository and cutting production over to
it on the owner-designated QA environment. Each numbered section ends on a check that tells you the step
is done.

Non-goals: the SDK relocation itself (#8851), non-collector repository moves
(#8871-#8879 have their own tasks), and the retired-coverage revival recipes
owned by follow-up issues (precedent: #9288).

Rule: every cutover task under #6707 must still name its own source parity,
telemetry, credentials, version pairs, and rollback disposition. This runbook
gives you the shape; the evidence on your issue proves your lane.

Check: you can state your lane's destination repo, QA activation name,
and which #6707 row it closes before you start section 1.

## 1. Start from the template repo

Cut the satellite from the template repository established in #6710 (landed
via #6731). The template carries the bounded-contract layout, the image lane,
and the review checklist; starting elsewhere re-derives all three.

The template excludes the cutover itself: deploy, grant, parity proof, and
removal below are per-lane work, not template content.

Check: `git log` on your satellite repo shows the template import as its
first commit.

## 2. Port to the bounded contract

The satellite observes provider truth and emits versioned facts through the
bounded SDK stdio contract. Core keeps admission, canonical identity,
projection, persistence, recovery, bootstrap, and repair; the satellite MUST
NOT duplicate any of them.

Keep schemas, stable keys, source scope, generations, confidence, provenance,
and redaction identical to the in-tree producer on representative and hostile
inputs. Prove it with source-parity tests in the satellite repo before any
deploy.

Check: the satellite's conformance suite passes against the same parity
fixtures the in-tree producer used, and no core-owned package is vendored
into the satellite.

## 3. Build and release an independent image

Each satellite ships its own image lane (precedent: independent amd64 image
for terraform-state). Tag releases per satellite repo convention and record
artifact provenance (registry, digest, supported oldest/newest core and
component version pair).

Check: the release digest is quoted in the cutover issue and resolves from a
clean pull.

## 4. Deploy to QA through iac-eks-eshu

Deploy via the GitOps repo (`iac-eks-eshu`), not `kubectl`, with ArgoCD sync
(prune included). The owner merges and syncs deployment PRs; agents hold at
read-only cluster access unless the cutover issue grants more, and MUST NEVER
create a dual-producer situation (old and new producers claiming the same
scopes).

First failure mode on record: the #8868 satellite pods entered
`launch_failure` at 00:43 on cutover day. Root cause was missing migrations,
not satellite code (owner comment on #8868, 2026-10-27T00:55Z). Pending
post-migration claims cleared the terminal failures. Check your lane's
migrations before you blame the satellite.

Check: ArgoCD reports the activation synced and healthy, and the coordinator
reaches Running with the satellite as the only producer for its scopes.

## 5. Hold an exact producer grant

The satellite claims scopes through an exact, per-activation grant:
precedent is a 5-key grant with a per-activation `serviceAccountName`
(#8868 production note). For AWS lanes this rides the cross-account IAM role
(IRSA); confirm the role in the iac repo before cutover. The grant names the
satellite and nothing else, so revoking it restores the pre-cutover posture.

Check: scheduled claims complete first-try on the satellite (precedent: all
day 04:18-17:50Z on #8868, 320 facts on the representative run, factless runs
converging with `finished_at` set), and no other producer emits the same
kinds.

## 6. Prove parity criteria 3-6 on the extracted path

Quote the cutover gate's proof criteria on your cutover issue (from the
#8852/#8868 acceptance records):

3. Source parity on representative and hostile inputs; schemas, stable keys,
   source scope, generations, confidence, provenance, and redaction preserved.
4. Exact admitted facts, reducer projection, canonical graph, and HTTP API /
   MCP truth on the extracted production path.
5. Concurrent retry, duplicate delivery, ordering, cancellation, revoked
   authority, stale claims, and partial/ambiguous failures without lost facts.
6. Bounded resource use and no regression on the touched path, with health,
   readiness, freshness, metrics, traces, structured logs, and status.

Tier-1-style unit proof alone is not enough: criterion 4 MUST run against
the satellite-claimed ingestion path (claim/scope/fact-shape drift), not
handcrafted facts. Second failure mode on record: the tier-2 drift E2Es did
not survive extraction unmodified (the satellite initially had no operator
S3-endpoint config and no fixture mode), which forced the #9288 revival lane.

Check: each of criteria 3-6 has linked evidence from QA on your cutover
issue, and credential handling, artifact provenance, and vulnerability
response are recorded (criterion 7).

## 7. Remove the superseded in-tree producer

Only after production acceptance, delete the in-tree producer and repoint
every registry, contract, spec, and doc that named it. Precedent scale:
#9312 removed on the order of 200 files (roughly +500/-13,000 lines).
What moves vs what stays:

| Moves to the satellite (delete in-tree) | Stays in core (do not touch) |
| --- | --- |
| `go/cmd/collector-<name>/` binary, config, service, record mode | `go/internal/collector/` shared kernels still imported by live code |
| Fixture estates and compose tiers that only the producer used | Storage, coordinator, parser, scope, facts packages |
| Verify scripts for the retired in-tree paths | Telemetry rows for kept instruments |
| Product-image build lines for the binary | Chart templates rendering the satellite source (with comments) |

The removal record calls this ownership rule 5: keep every package still
imported by live core code, verified by a whole-tree reference census with
zero dead files; re-home the rest or delete it. Fact producer records,
fact-kind families, surface overlays, replay manifests, golden-corpus specs,
env registries, Helm comments, telemetry rows, and skill baselines all need
repointing in the same PR.

Check: `go build ./...` clean, `go vet` on touched packages clean, replay
coverage `--blocking` passes with zero stale, and the registries verify clean.

## 8. Rehearse rollback, or record the waiver plus a revival follow-up

Each cutover needs a rollback disposition before its removal PR. Either
rehearse the rollback (restore the in-tree producer at zero replicas,
re-register, verify fresh facts), or record all three waiver elements, per
the #8868/#9288 precedent (mechanism only, not a blanket grant):

1. The owner's explicit roll-forward-only order, quoted on the cutover issue.
2. The waived rehearsal named on the acceptance record (criterion 9).
3. A revival follow-up issue filed before the removal merges, owning every
   retired-not-carried proof (precedent: #9288 filed before #9312 merged).

Check: the removal PR body cites the rehearsal evidence or all three waiver
elements with issue links.

## 9. Write the retired-coverage honesty note

Every removal PR MUST state what is now invisible. Precedent wording
(#9312 record): tier-1 plus satellite conformance is NOT equivalent to the
retired coverage; name the retired suites, what regression shape they alone
could catch, and what now catches it (satellite conformance, live ops
signals) or does not. A gap that is accepted is recorded; a gap that is
silent is a defect.

Check: the removal PR body and the evidence record both name the retired
suites and the accepted gap.

## Appendix: precedent index

- #8852 (aws-cloud) and #8868 (terraform-state): closed 2026-10-28 with
  deployed QA proof; closing production comments quote live facts.
- Removal: #9312; survey record
  `docs/internal/evidence/8852-8868-collector-removal.md` (scope table,
  verification matrix, revival recipe, coverage gap, waiver record).
- Revival follow-up: #9288 (tier2/v25 drift E2E on the satellite, closed
  2026-10-28).
- Remaining satellite lanes read this first: #8875-#8879 and the open
  collector cutovers under #6707.
