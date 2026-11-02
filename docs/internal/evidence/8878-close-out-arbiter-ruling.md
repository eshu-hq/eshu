# Arbiter ruling: #8878 close-out (box 10, QA re-pin, #9383, #9385)

Date: 2026-11-02. Arbiter: arbiter-eshu. Issues: #8878, #9383, #9385.
Mode: read-only; no files edited, no GitHub writes by the arbiter.

## Summary

- Q1: A. Port the component lane into eshu-scanner-worker and re-pin from its
  own attested publish. Option B is refused.
- Q2(i): GO only as an atomic pair with a post-#9744 main image.
- Q2(ii): GO as a bounded, supervised window.
- Q3: #9383 stays open per boundary. #9385 closes with the Q1-A port PR, not
  before.
- Q4: docs correction first and now, port second, QA re-pin third.

## Verified inputs

Commands ran from the eshu checkout after `git fetch origin`; `origin/main` was
`f12ebc5f04`.

- #9741, #9744 and #9795 are merged (merge commits `a4df1d2c9f`, `2866b2a46f`,
  `707a1aacef`). `go/go.mod` on `origin/main` pins `eshu-sdk/collector
  v0.2.0-rc.3`.
- `scannercomponent/manifest.go` pins `Scanner-Publish-SHA:
  a371d1e21f6ba013925ea5d263d8de6c6b54d428` with `Scanner-Publish-Repo:
  eshu-hq/eshu`. The pinned images were built from eshu's own `Dockerfile`
  stages `scanner-image`, `scanner-sbom` and `scanner-osruntime` and the
  `build-and-push-scanner-images` job in `docker-publish.yml`. #9741 deleted
  both. `docker-publish.yml:312` says the builds moved to the eshu-scanner-worker
  satellite.
- eshu-scanner-worker `origin/main` is `2d071e6` (37 files). Its `go.mod`
  requires only `go-sqlite` and `go-rpmdb`. It has `scannerworker/{doc,analyzer}.go`
  and the `ospackagevulnerability` parsers, no `cmd/`, no `Dockerfile`, no
  publish workflow (`.github/workflows/` holds `ci.yml` only), no `SECURITY.md`.
  `INTERIM.md` lists the claim loop, `imageanalyzer/`, `sbomgenerator/`,
  `osruntime/`, `envelope.go`, `scannerworker/component/` and the three
  `scanner-component-*` commands as "staying in-tree". `go/boundary_test.go`
  fails the build on any `github.com/eshu-hq/eshu/` import. Today no repository
  can build the three scanner images.
- Port size at `a4df1d2c9f^` (non-test Go lines): `scannerworker` 6434,
  `ospackagevulnerability` 2361, three component commands 57 each;
  `go/cmd/scanner-worker` 1263 (standalone worker, not needed for the component
  lane). Core-internal imports the component lane must replace: `internal/facts`
  x16, `internal/scope` x9, `facts/supply/chain` x5, `packageidentity` x4,
  `internal/component` x3, `buildinfo` x3, `factenvelope` x2, `urlredact` x1.
  `component/runner.go:208` converts through `factenvelope.SDKFactFromInternal`,
  so the wire output is already the SDK `collector.Result` with
  `Page`/`PageContinuation`. eshu-sdk already publishes `factschema/scannerworker`,
  `factschema/sbom` and `factschema/vulnerability`; it has no `scope` or
  `packageidentity` package.
- The removal ruling record (#8878 comment 6545699210, 2026-10-31) lists the
  QA artifact links and the rollback disposition. It does not mention
  independent build, release, or box 10. The design doc "Per-repository cutover
  gate" (`docs/internal/design/eshu-ecosystem-repository-migration.md`) requires
  "independent build, release, deployment, and ownership", and "Migration
  sequence" step 8 removes in-tree code "only after production cutover proof
  passes". `docs/internal/satellite-cutover-runbook.md` section 7 says the same.
- False wording on `origin/main`:
  `go/internal/collector/scannercomponent/README.md:9-10` ("lives in the
  eshu-hq/eshu-scanner-worker satellite"), `doc.go:13`, and
  `specs/surface-inventory.v1.yaml:201-204` ("collection runs from
  eshu-hq/eshu-scanner-worker").
- Seed-versus-pin coupling: eshu-deploy issue #20 (open) documents, with
  `TestLoadConfigDeniesDriftedScannerActivation`, that a seeded `eshu.scanner.*`
  digest that differs from the binary's manifest pin makes `VerifySeededArtifact`
  fail, `LoadConfig` return the error, and the workflow coordinator process exit.
  `scannercomponent/reconcile.go:20-25` refuses the activation on any mismatch
  before any claim or execution. eshu-deploy
  `helm/eshu/ci/scanner-activations.values.yaml` on `origin/main` (`c7bd9f4`)
  still pins the `sha-17e42456aa` digests (`2c9269e9`, `2d3b72aa`, `6ff09b67`).
- #9383 has one comment, the owner policy order of 2026-10-28 (no downgrade
  migration rehearsals; delete-path for stateless satellites; per-boundary
  disposition recorded as delete-path-suffices or roll-forward-plus-rebuild with
  a documented re-sync RTO). Dispositions on the cutover issues: #8878 (comment
  6545699210), #8879 (deferred by arbiter ruling 2026-10-28, no boundary). #8875,
  #8876 and #8877 have no disposition comment beyond progress notes.
- #9385 has four comments; the last records `collector/v0.2.0-rc.3` publication
  and the open eshu-sdk changelog-dating follow-up, which has no PR. Criterion 3
  evidence is the offline re-run (comment 6531830010) on the pre-removal in-tree
  analyzers.

## Q1. Box 10 (independent build, release, deploy): A

Decision 1. Port the component lane into eshu-scanner-worker and re-pin from its
publish. Option B is refused.

The a371d1e21f images were compiled inside the eshu module from `go/internal/*`
packages, so they embody core implementation handles at build time. Declaring
them immutable artifacts would not downgrade box 10; it would mark a never-met
criterion as met. The cutover gate and runbook section 7 name independent build
as a requirement.

- The removal ruling of 2026-10-31 did not settle box 10. Removal before the
  destination could build inverted migration step 8. That is a sequencing defect
  to fix forward. Do not revert #9741.
- Supply chain: the only buildable provenance for the running analyzers is a
  deleted tree in eshu history. A vulnerability in `go-rpmdb`, `go-sqlite` or an
  analyzer parser cannot be fixed and republished by any repository. That is a
  box 9 failure as well as a box 10 failure.
- Scope of A is the component lane only (about 8.9k non-test Go lines plus three
  57-line mains), not the standalone `go/cmd/scanner-worker`, which needs
  `internal/workflow` and stays gated on #8877 and #9376 per `INTERIM.md`. The
  lane must replace `facts`, `scope`, `packageidentity`, `facts/supply/chain`,
  `factenvelope`, `internal/component`, `buildinfo` and `urlredact` with eshu-sdk
  types or satellite-local copies; `boundary_test.go` is the gate. This is not a
  mechanical copy. Budget two to four days with an explicit time-box, and prove
  the theory first: port `component/paging.go` plus one analyzer, make
  `boundary_test.go` and the golden parity envelopes
  (under `go/internal/collector/extensionhost/`, directory `scanner-parity`) pass, then
  port the rest.
- Required proof for the port PRs in eshu-scanner-worker: (a) `go test ./...`
  including `TestImportBoundaryRejectsCoreImports` green; (b) byte-identical
  `collector.Result` pages against the committed `scanner-parity` golden
  envelopes for all three analyzers; (c) the #9380 hostile full-range re-run as a
  committed test (50,000 facts per analyzer paged under the 4,128,768-byte
  bound, reassembly identical); (d) a `Dockerfile` with the three stages and a
  publish workflow that pushes `ghcr.io/eshu-hq/scanner-{image,sbom,osruntime}`
  by digest with SLSA provenance attestation from `eshu-hq/eshu-scanner-worker`;
  (e) `SECURITY.md` and a version-pair table (box 9).
- Required proof for the eshu re-pin PR: `Scanner-Publish-Repo` flips to
  `eshu-hq/eshu-scanner-worker` with the new anchor and digests;
  `scripts/verify-scanner-digest-pins.sh` passes with attestations verified
  against the satellite repo; `TestLoadConfigDeniesDriftedScannerActivation` and
  its mirrored copies are updated red first.
- Flip condition: A flips to B only if the owner writes, on #8878, an explicit
  acceptance that box 10 is waived for this boundary with a revival follow-up
  issue filed (the #8868 and #9288 waiver mechanism, all three elements). Box 9
  stays unmet under B.

Decision 2. Correct the false "lives in the satellite" wording now, in its own
docs PR, regardless of Q1 timing. Accuracy is the first rule of the repository;
`README.md`, `doc.go`, `specs/surface-inventory.v1.yaml` and
`docker-publish.yml:312` state something no repository tree supports.
Replacement wording: "Execution sources were removed from eshu in #9741; the
pinned images were built from eshu commit a371d1e21f; the destination port into
eshu-hq/eshu-scanner-worker is in progress (#8878 box 10)." Update the same
sentence in the satellite's `INTERIM.md` in the port PR. Run the docs build and
`git diff --check`. The PR touches a spec file, so it takes the normal review
path.

## Q2. Irreversible-act review: QA re-pin and unpause

Decision 3. Q2(i) GO, with one hard condition: the seed re-pin to
`450f81b8`/`76d36e27`/`04eea122` must land in the same ArgoCD sync as a
main-image bump to an eshu build that contains #9744 (any image from
`2866b2a46f` or later; `f12ebc5f04` is current). The live main image
`sha-bdf28233` predates #9744 and carries the pre-#9744 manifest pins, and a
seed/pin mismatch exits the workflow coordinator. Re-pinning seeds alone at
replicas 0 would take the coordinator down on the next restart. The pairing is
atomic in both directions: the main bump cannot go first either.

Pre-conditions before the IaC and eshu-deploy PRs:

1. Confirm the three live seed values are 64-hex sha256 digests, not publish-SHA
   tag prefixes. Comment 6545156825 (2026-10-31) says `0945164c`/`19b1def7`/
   `9f81bd96` were #9714 publish-SHA prefixes that were "Not used", and that IaC
   #483 seeded `552d9087`/`fcd10e5e`/`40d93b0e`. The audit reports the live seeds
   as `0945164c`/`19b1def7`/`9f81bd96`. One is wrong. Read the live ConfigMap or
   Secret and the IaC file and reconcile before writing the PR. If the live
   values are prefixes, say so in the re-pin PR.
2. `crane digest ghcr.io/eshu-hq/scanner-<name>:sha-a371d1e21f6b` equals each pin
   and `gh attestation verify oci://... --repo eshu-hq/eshu` exits 0 for all
   three. Re-run, do not reuse.
3. A pre-authored revert PR for both repos (the #448/#449 pattern) linked on
   #8878 before merge.
4. eshu-deploy: bump `helm/eshu/ci/scanner-activations.values.yaml` and
   `go/internal/runtime/helm_scanner_activations_contract_test.go` together. The
   PR closes eshu-deploy #20 only if its body says so with `Closes`.
5. ArgoCD auto-sync stays disabled. Run one manual sync, wait for the workflow
   coordinator to roll and log a clean `LoadConfig` with no `VerifySeededArtifact`
   error. Scanner Deployments stay at spec 0 with no pods.

Abort and rollback: merge the pre-authored revert PRs and re-sync. The end state
is the prior main image and seeds with the coordinator healthy. Rollback is a
pure configuration revert with no data path change.

Decision 4. Q2(ii) GO as a bounded, supervised window after Q2(i) has been
Synced and Healthy for at least one coordinator poll cycle (the coordinator logs
"skipped duplicate workflow work" every 30 minutes, so 30 minutes is the floor).
Scale the three activations to 1 through a reviewed IaC PR, observe for no more
than 20 minutes, then merge the restore PR (replicas 0) and re-sync.

Required reads during the window: the
`/api/v0/supply-chain/impact/findings?subject_digest=...&image_ref=...` two
stable reads with the full envelope (analysis 1, os_package 28,
`missing_evidence` exactly `[advisory_sources]` unless the vulnerability
intelligence feed has landed); the SBOM attachments read with `component_count`
1056 and the warning reconciliation line (committed count 314 versus
`warning_summary_count` 315, occurrence sum stated, group with count 2 named);
the same values through MCP `list/count_supply_chain_impact_findings` and
`get_sbom_attestation_attachment_inventory` on the serving `eshu-mcp` pod; and
`kubectl top pod` at least twice (the missing box 8 measurement). Zero new facts
for the QA scope is the expected store outcome.

Abort triggers: any `failure_class`, `invalid_result` or `ERROR` line in a
scanner pod, a coordinator restart, a pod restart, a served-count change versus
the pre-window baseline other than feed-driven `latest_observed_at`, or a
scanner pod exceeding its manifest resource limit. Abort action: merge the
restore-to-0 PR immediately, re-sync, verify spec 0 and no pods, then re-read the
API once.

A standing unpause (replicas left at 1) needs a separate owner decision. It is a
production traffic authority change; the existing consent covers deploying to
QA and the bounded window pattern, not leaving it on.

Decision 5. After Q1-A lands, QA re-pins again to the satellite digests. The
a371d1e21f re-pin is still correct now because the main image must advance for
#8876 and the other lanes. Record it as an interim pin in the PR body.

## Q3. #9383 and #9385

Decision 6. #9383 stays open. Its acceptance is a disposition recorded per
boundary on each cutover issue (#8875 through #8879). Recorded today: #8878 and
#8879. A policy link does not satisfy a per-boundary checklist. Close #9383 when
each of the five cutover issues carries a disposition comment naming
delete-path-suffices or roll-forward-plus-rebuild with a re-sync RTO, the
artifact digest, topology and evidence location, with the owner policy quoted
once in the closing comment.

Decision 7. #9385 closes with the Q1-A port PR, not before and not on the
offline record alone. Criteria 1, 2 and 4 are met on `origin/main`. Criterion 3
(analyzer byte accounting lands against the new contract; the #9380 feasibility
re-run passes full-range on all three analyzers) rests on `component/paging.go`,
which #9741 deleted, so no buildable tree carries it today. When the port PR
carries `paging.go` and the full-range re-run as a committed test in
eshu-scanner-worker CI, criterion 3 is met and #9385 closes with a comment
citing #9395, rc.3, #9795, the 2026-10-30 re-run (comment 6531830010, naming the
commit it ran against) and the satellite test. The eshu-sdk changelog-dating
follow-up is a P3 docs nit and does not gate closure. If the owner accepts B
under Q1, #9385 closes on the offline record with a note that the analyzer side
is unbuildable.

## Q4. Order of remaining #8878 work

Decision 8.

1. Now, in parallel with everything: the docs correction PR (Decision 2). Also
   post the warning reconciliation line (314 committed versus 315 summary) on
   #8878, since the 2026-10-30 ruling required it on record.
2. Now, in parallel with #8875 and #8876, no cluster access needed: the Q1-A port
   lane in eshu-scanner-worker (theory-proof step first). It touches no eshu
   paths and no QA state.
3. QA, serialized with the #8876 and #8875 QA windows (shared cluster,
   coordinator and manual-sync lever): Q2(i) paired re-pin plus main-image bump,
   then Q2(ii) bounded unpause with the API, MCP and `kubectl top` reads. One
   window owner at a time; not during an #8875 synthetic-write window or while
   the #8876 authority switch is mid-change.
4. After the port publishes attested images: the eshu re-pin PR (anchor flips to
   the satellite), IaC and eshu-deploy re-pin, and one more bounded window. That
   window is the box 6 and box 7 extracted-path evidence (retry; cancellation by
   a deliberate pod delete mid-claim; stale claim via a second activation at
   replicas 2 for one claim; partial failure via a malformed input in
   `scripts/scanner-worker-seed-matrix.sh`). The box 9 record (`SECURITY.md`,
   credential statement, version-pair table) lands in the satellite with the
   port.
5. Close-out: a box-by-box comment on #8878 with the evidence links, then #9385,
   then #8878. #9383 closes separately when Decision 6 is met.

Decision 9. The executor lane's commit `176d289681` (a stale reference in the
SBOM `EXTRACTION_PREP.md`) is accurate and may ride with the Decision 2 docs PR.

## What the arbiter will not do

- Accept option B: it relabels an unmet, safety-relevant criterion as met.
- Treat the 2026-10-31 removal ruling as having settled box 10.
- Approve a seed re-pin on its own: the fail-closed coupling exits the
  coordinator.
- Grant a standing unpause.
- Close #9383 on the policy link, or #9385 on evidence from a deleted tree.

## NOT_CHECKED

- Live QA state (ArgoCD Synced/Healthy, replicas 0, live main image tag, live
  seed values, the 30-minute coordinator log line). The arbiter had no cluster
  access; this rests on the audit lane's read-only report. The seed-value
  contradiction (precondition 1) must be reconciled before any write.
- The full text of the 2026-10-31 removal ruling. Only the summary comment
  6545699210 is on GitHub, and no `docs/internal/evidence/8878-*` file existed on
  `origin/main`. If the full text waives box 10 explicitly, quote it on #8878 and
  the Q1 flip condition applies.
- `gh attestation verify` on the three a371d1e21f digests: not run here.
- Whether eshu-sdk exposes enough of `scope` and `packageidentity` semantics for
  the port, or whether the satellite must carry local copies. The theory-proof
  step in Decision 1 determines it.
- The live IaC `origin/main` scanner seed file and PRs #483, #485 and #491.
- The eshu-mcp serving pod image on QA today. Re-read before the Q2(ii) MCP
  pass.
