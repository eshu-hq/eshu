# #10071 OCR proof image re-pin diagnosis (lane report 2026-11-05)

## Scope

Base: `origin/main` at `0650a6a4d2`. Worktree
`/home/ubuntu/personal-repos/eshu-10071-ocr-lane`, branch
`drive/10071-ocr-repin`. Answers #10071 acceptance criterion 3 (root
cause, rejected hypotheses, remediation options). Moves no product code;
touches `scripts/ocr-sandbox/Dockerfile`, `scripts/prove-ocr-sandbox.sh`
(per-arch digests), the new weekly `ocr-sandbox-proof.yml` schedule, and
this record.

## Root cause

The Alpine 3.21 repository index now serves only `musl-1.2.5-r12` and
`zlib-1.3.2-r1` on both `x86_64` and `aarch64` (verified by direct
`APKINDEX.tar.gz` fetch from `dl-cdn.alpinelinux.org` on 2026-11-05;
single `P:musl`/`P:zlib` records per arch, stable across six probes).
The proof Dockerfile still pinned `musl=1.2.5-r11` and `zlib=1.3.2-r0`.

The failure mode is subtler than "docker build fails closed". The
digest-pinned base (`alpine:3.21@sha256:48b0...`) preinstalls
`musl-1.2.5-r11` and `zlib-1.3.2-r0` (verified with `apk list
--installed` in the bare base). `apk add` with an exact `=` pin that the
installed set already satisfies is a no-op: three fresh `--no-cache
--pull` builds on 2026-11-05 installed r11/r0 without error, and the
proof then passed against the old digests. So the image builds green
while running stale libc/zlib — the exact-pin design ("a version that
leaves the index fails the build instead of floating") does not hold
for packages the base layer already carries. The real breakage lands
when the base digest is refreshed or a non-preinstalled pin leaves the
index. No CI job builds this Dockerfile, so the drift was silent either
way.

## Rejected hypotheses

- **CDN edge variance.** Six consecutive `APKINDEX.tar.gz` fetches plus
  a raw record dump all showed exactly one `musl` record (`1.2.5-r12`)
  and one `zlib` record (`1.3.2-r1`) per arch. The index is stable, not
  split-brain.
- **Dual-version index.** `grep` over the unpacked `APKINDEX` shows a
  single record per package; `apk policy` in-container lists r11/r0
  with source `lib/apk/db/installed` (the base layer) and r12/r1 with
  source the repository URL. The repo keeps one generation.
- **Stale docker layer cache.** The pass reproduced with `--no-cache
  --pull` and a fresh build context, and `apk list --installed` in the
  fresh image confirmed r11/r0. Cache was not the cause; the base-image
  preinstall was.
- **Other pins drifted too.** The rebuilt proof image resolves every
  other pin (`tesseract-ocr=5.5.0-r0`, `leptonica=1.84.1-r0`,
  `libgcc=14.2.0-r4`, …) unchanged: the tesseract binary and
  `eng.traineddata` digests are identical before and after, and only
  the 15-file library-set hash moved (libc/libz contents).

## Remediation options

- **Re-pin musl/zlib, refresh digests (chosen).** Two-line Dockerfile
  change; per-arch `EXPECTED_LIBS_SHA256` re-measured from real builds
  (binary and model digests unchanged). Minimal, keeps the exact-pin
  posture for everything the base does not shadow.
- **Refresh the Alpine base digest too (rejected for this lane).** A
  newer `alpine:3.21` would preinstall r12/r1, but it also re-renders
  fixtures and moves every digest; that is a separate proof cycle, not
  part of #10071.
- **Extend `verify-apk-floors.sh` to audit `=` pins (rejected).** The
  verifier requires `name>=version` floors and fails closed on exact
  pins ("the guard would be vacuous"), and an index audit cannot
  distinguish "stale but stuck in the base layer" from "broken" — only
  a build can. Touching the shared guard for this would add risk for
  no signal.
- **Weekly scheduled proof run (chosen).** New `ocr-sandbox-proof.yml`
  runs `prove-ocr-sandbox.sh --platform linux/amd64` on `ubuntu-24.04`
  every Wednesday 06:30 UTC plus `workflow_dispatch`. Standalone
  workflow (not a `static-contract-gates.yml` matrix entry) per the
  bespoke-trigger exception, following the `apk-floors-drift.yml` /
  `scanner-freshness.yml` precedent. No `pull_request` trigger, so it
  never blocks unrelated changes.

## Proof

- RED: after the pin edit, before the digest refresh,
  `prove-ocr-sandbox.sh --platform linux/arm64` failed closed on the
  library-set digest (`012ca858… != 5556b1aa…`), binary and model
  digests unchanged.
- arm64 library-set digest independently recomputed with the script's
  `LIBRARY_LISTER` formula outside the script: `012ca858…`, match.
- New pins: `EXPECTED_LIBS_SHA256[arm64]=012ca858…`,
  `[amd64]=74ed1eae…`; binary/model pins unchanged on both arches.
- `prove-ocr-sandbox.sh --platform linux/arm64`: exit 0,
  `PASS - OCR sandbox proof on linux/arm64`.
- `prove-ocr-sandbox.sh --platform linux/amd64` (qemu-user on arm64
  host): exit 1 — NOT a code regression. All three digest checks passed
  (binary `38b441…`, model, libs `74ed1eae…` over 15 files), and 6 of 7
  fixtures proved deterministic with no canary leak. The heaviest
  fixture (`key-body.png`, 0.83 s native) exceeded the fixed 20 s OCR
  timeout under qemu-user's ~30× slowdown (sibling fixtures measured
  6.5–11.2 s vs 0.2–0.3 s native; test ran 311.17 s, `context deadline
  exceeded`). Full amd64 proof is therefore infeasible on this
  emulation host without weakening the timeout bound, which is out of
  scope; the weekly CI job runs `--platform linux/amd64` natively on
  `ubuntu-24.04`, where the bound holds.
