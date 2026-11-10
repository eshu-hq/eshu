# C3 deployment config-source record — QA environment (Refs eshu-hq/eshu#8875)

Production target: the owner-designated QA EKS environment. The
private IaC repository stays opaque; only its pointer and the
live-verified state below are recorded here.

## Config source pointer

- Repository: the private IaC repository (deploy authority; not read
  from this lane).
- Overlay path: the QA overlay in the GitOps tree.
- Current refs (merge commits, never branches):
  - Revert-to-dark: IaC PR #641, merge `fca3f738` (7 files, 487
    deletions, 0 additions; inverse of the enable change).
  - Staged-audits flip: IaC PR #625, merge `1166602b`
    (`ESHU_POSTGRES_READ_ROUTING=staged-audits`).

## Live-verified state (2026-11-10, read-only cluster reads, exit 0)

- QA GitOps application: Synced.
- Satellite image:
  `ghcr.io/eshu-hq/eshu-workflow@sha256:10c4bcbe659a3044961abc05199b6677202123bb1848c6ffc60660f3f748a96c`
  (full digest, re-verifiable per digest; unchanged, contention proofs
  stay bound).
- Resolution engine: `ESHU_POSTGRES_READ_ROUTING=staged-audits`,
  1/1 available.
- Isolated plane: the name-filtered census (`kubectl get
  deploy,job,cronjob -n eshu -o name | rg -c isolated`) returns 0 in
  namespace `eshu`. That proves zero isolated-named objects, not zero
  objects by any name; the removal itself is the file deletion in IaC
  PR #641 with the application Synced after.

## Known non-blocking flag

Application health reads Degraded from two stuck rollouts on an
unpullable collector image from another lane's pin change; both
serve at half replicas. Not a config-source defect.

## Staleness

Re-verify this record on any pin, overlay, or target change: a new
merge commit re-opens C3 until the live state above is re-read.
