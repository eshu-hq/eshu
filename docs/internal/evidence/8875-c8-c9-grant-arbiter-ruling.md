# Arbiter Ruling: #8875 C8 Ownership, C9 Bar, Grant Path

Date: 2026-11-05. Arbiter: `arbiter-eshu`. Requested by the #8875 drive
coordinator after two verification sweeps confirmed the remaining gaps are
ownership and scope decisions that evidence alone cannot settle.

## Raw inputs

- Re-verdict 2026-11-05T17:30Z on #8875: C4/C5/C6 met; C1/C2/C3/C8/C9/C10/C11/C12
  partial; C7 open.
- Live test-cluster reads (read-only kubectl, exit 0): claim-plane + satellite both digest
  `e1b564af`, 1/1 ready, satellite dark with claims off on both synthetic
  instances; in-tree coordinator active, claims on.
- Live `/metrics` scrape of claim-plane v0.1.1 (read-only port-forward,
  exit 0, 25 lines): exactly 4 families (`grants`, `mutations`, `reaps`,
  `queue_depth`). No histogram. Confirms the committed NOT_AVAILABLE note.
- `gh attestation verify` on `oci://ghcr.io/eshu-hq/eshu-workflow:v0.1.1`
  (digest `sha256:e1b564af…e47a4`): exit 0. Two DSSE statements: SLSA v1
  provenance (builder `image-publish.yml@refs/tags/v0.1.1`, commit `8311ed69`,
  run 41332377147) + SPDX SBOM.
- #9727: OPEN, window closed 2026-11-04, contention/stale-fence/reclaim
  NOT_PROVEN. No grant newer than the 2026-11-04T16:46Z authority comment.
- #9376: OPEN. Satellite deploy change + synthetic-write grant outstanding.
  `eshu-deploy @696793e` has no satellite target.
- Host `cosign` verify NOT_CHECKED: no binary on the drive host.

## Rulings

1. **C8 owner is the #9376/#9727 contention lane, not a new lane.** Skew and
   commit-p99 exist to size the fence margin for that lane's flip; throughput
   runs in its window. The coordinator records this assignment on #8875; the
   lane accepts or the slice stays flagged.
2. **C8 read-only leg (no grant, execute now):** §6.2 of
   `docs/internal/design/9376-claim-plane-commit-fence.md` as written — skew
   via option A→B→C in that order, commit p99 from collector logs with `n`
   recorded, verdict `2*(skew_bound+commit_p99) <= 5s`. Zero samples is a
   failed measurement, not a pass. Also record ambient client-side p99 from
   `eshu_dp_workflow_claim_plane_request_duration_seconds`. Week-over-week
   stays waived (owner, 2026-11-05).
3. **C8 grant-gated leg:** the binding throughput comparison (same metric
   boundaries, digest, topology per `REMOVAL_PREP.md` §4) runs once inside the
   next #9727 window under that window's grant. No separate C8 synthetic-write
   grant is pursued. The server-side histogram gap becomes a satellite-repo
   telemetry issue; it does **not** block C8 closure but does block any future
   server-latency SLO claim.
4. **C9 closure bar is record-and-verify, as built.** The criterion requires
   provenance *recorded*, not a SLSA level. Verified v0.1.1 attestation +
   merged vuln-response/version-matrix + digest-pinned deploy satisfies C9.
   No L3, hermetic build, or GitHub Release provenance is required.
   Conditions: (a) the record states the level honestly — hosted-builder
   provenance, no L3 claim in docs or capability specs; (b) v0.1.0
   (`MANIFEST_UNKNOWN`) is recorded unverifiable and barred from deploy pins.
   The verifier gate is a runbook step (`gh attestation verify` on the exact
   digest before pinning), not new generator machinery.
5. **Grant path: owner `linuxdynasty` alone approves.** This review satisfies
   the pre-act review requirement; it neither issues nor widens a grant. The
   coordinator posts **one bundled request** on #9727, cross-linked from #9376
   and #8875: next flip window (contention + stale-fence + reclaim + C8
   throughput leg), #9376 satellite deploy change, deployed snapshot rehearsal
   writes — each with digest `e1b564af`, abort watches, residue 0/0, and
   return-to-dark. Split requests across lanes are denied as a pattern.
6. **Preconditions before that window:** §6.2 verdict posted (ruling 2); the
   11-04 isolation-probe failure (scheduler-seeded harness item) has a named
   fix or a redesigned probe; concurrency proof per repo rigor (full
   replay/retry matrix, contention observability, tracked evidence note).
   Serialization or claims-off "contention" evidence is rejected in advance.

## Required proof

- §6.2 raw samples + verdict on #8875.
- `gh attestation verify` exit recorded per deployed digest.
- Window evidence note naming the conflict domain, before/after counts, residue.
- C9 record with honest level wording, grep-checked against specs.

No-Regression Evidence: this file is a decision record only. It changes no
code, query, worker, lease, batch size, schema, metric, or deploy pin.

## Unverified at ruling time

Live `/metrics` and in-cluster state (taken from the coordinator record, not
re-scraped by the arbiter); private IaC PR bodies; `eshu-deploy` tree beyond
the path census; whether the #9727 lane accepts ruling 1.

## Addendum 2026-11-05: §6.2 read-only leg executed — VERDICT PASS

Runbook: `docs/internal/design/9376-claim-plane-commit-fence.md` §6.2
(read-only: `kubectl get`/`logs`, port-forward reads, Prometheus instant
queries; no cluster object created or changed). Epoch 2026-11-05 UTC.

- Skew (option A, Prometheus `monitoring-kube-prometheus-prometheus`
  port-forward, all queries exit 0): collector node
  `ip-10-247-204-70` 24h max offset 0.04508066177368164s, satellite node
  `ip-10-247-193-98` 24h max offset 0.0417783260345459s, timex offsets 0 on
  both with `sync_status=1`. `skew_bound = 0.0451 + 0.0418 + 0 = 0.0869s`.
- Commit p99 (21 running collector pods, `logs --since=24h` piped through the
  runbook `jq` for `commit_transaction`, all 21 kubectl+jq exits 0):
  conservative worst per-pod `commit_p99 = 0.168172777s` (cicd-run, n=28,
  p99=max). Pooled n=139 p99=0.101247025. `--previous` N/A (RESTARTS=0).
- Ambient client-side p99 of
  `eshu_dp_workflow_claim_plane_request_duration_seconds`: UNAVAILABLE —
  absent cluster-wide (Prometheus series count 0) and in all 21 pods' logs.
  Expected pre-flip (no collector talks to the plane yet); the verdict uses
  server-side `commit_p99` only. That omission is covered by headroom (PASS
  holds for any `commit_p99 <= ~2.41s`), and post-flip client-side latency is
  watched via `eshu_dp_ingestion_commit_pin_slack_seconds`.
- Verdict: `2 * (0.0869 + 0.1682) = 0.51s <= 5s`. **PASS** with ~4.49s of
  margin. Holds for any `skew_bound <= ~2.33s`.

Performance Evidence: §6.2 margin inputs measured on the test cluster per the
fence runbook; no code changed. Conflict domain: collector-vs-satellite clock
skew plus `commit_transaction` duration on the flipped collector set.
Observability Evidence: `node_time_seconds`/`node_timex_offset_seconds`
(node-exporter) and `ingestion commit stage completed` log durations; live
check after the flip is `eshu_dp_ingestion_commit_pin_slack_seconds`
(healthy 40–60s; any `deadline` rejection means margin or plane attention).
