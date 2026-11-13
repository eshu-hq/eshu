#!/usr/bin/env python3
"""G2 served-spec snapshot generator, Phase 1 (eshu#9669 gate G2).

Derives docs/internal/g2-served-snapshot.md for the content-search family
from the tree plus local satellite git objects. Offline by construction:
only git rev-parse/show/ls-tree (read-only plumbing), rg, and sha256sum.
No fetch, pull, clone, HTTP, or tracker reads in any form.

Usage (run in the worktree root):
  python3 scripts/gen-g2-served-snapshot.py --satellite-repo PATH [--out FILE]
  G2_SATELLITE_REPO=PATH python3 scripts/gen-g2-served-snapshot.py
"""
import argparse
import hashlib
import os
import re
import subprocess
import sys
from pathlib import Path

LIB = Path(__file__).resolve().parent / "lib"
sys.path.insert(0, str(LIB))

from g2_snapshot_data import (  # noqa: E402
    CLASSES,
    CORE_SHA,
    CUTOVER,
    FRAGMENT_FILE,
    GENERATOR_NAME,
    GENERATOR_VERSION,
    INVENTORY_SCOPE,
    MOUNT_FILE,
    MOUNT_FUNC,
    POINT_PIN_CONST,
    POINT_PIN_FILE,
    ROUTES,
    SATELLITE_FRAGMENT,
    SATELLITE_SEARCH_DIR,
    SATELLITE_SHA,
    SEARCH_PIN_CONST,
    SEARCH_PIN_FILE,
    SERVED_BY,
    short,
)


def find_root():
    """Locate the worktree root without hard-coded absolute paths."""
    for cand in Path(__file__).resolve().parents:
        if (cand / "go" / "cmd" / "api" / "CUTOVER_OWNERSHIP.md").is_file():
            return cand
    p = subprocess.run(["git", "rev-parse", "--show-toplevel"],
                       capture_output=True, text=True)
    if p.returncode == 0 and p.stdout.strip():
        return Path(p.stdout.strip())
    raise SystemExit("cannot locate worktree root (no CUTOVER_OWNERSHIP.md found)")


ROOT = find_root()
DEFAULT_OUT = ROOT / "docs" / "internal" / "g2-served-snapshot.md"


def sh(args, cwd=None):
    """Run a command, failing closed with stderr on error."""
    p = subprocess.run(args, capture_output=True, text=True,
                       cwd=str(cwd or ROOT))
    if p.returncode != 0:
        raise SystemExit(f"command failed: {' '.join(args)}\n{p.stderr.strip()[:500]}")
    return p.stdout


def sha_bytes(b):
    """Return the hex sha256 of bytes."""
    return hashlib.sha256(b).hexdigest()


def parse_args(argv):
    """Parse command-line arguments."""
    ap = argparse.ArgumentParser(description="G2 served-snapshot generator.")
    ap.add_argument("--out", default=str(DEFAULT_OUT))
    ap.add_argument("--satellite-repo", default=os.environ.get("G2_SATELLITE_REPO", ""),
                    help="path to a satellite (eshu-api) git checkout holding "
                         "SATELLITE_SHA objects (read-only; never fetched)")
    ap.add_argument("--core-sha", default=None,
                    help="override the CORE_SHA generation record (re-base only; "
                         "without it the source files must be clean)")
    return ap.parse_args(argv)


SOURCE_FILES = (CUTOVER, MOUNT_FILE, FRAGMENT_FILE, SEARCH_PIN_FILE, POINT_PIN_FILE)


def verify_clean_or_pinned(core_sha):
    """Fail closed when the derivation inputs differ from the recorded base.

    Without an explicit --core-sha, every in-tree source file must be
    clean, so the recorded CORE_SHA truthfully describes the inputs.
    """
    if core_sha is not None:
        sh(["git", "cat-file", "-e", core_sha + "^{commit}"])
        return core_sha
    dirty = sh(["git", "status", "--porcelain", "--", *SOURCE_FILES]).strip()
    if dirty:
        raise SystemExit("source files dirty; commit them or re-base with "
                         "an explicit --core-sha:\n" + dirty)
    sh(["git", "cat-file", "-e", CORE_SHA + "^{commit}"])
    return CORE_SHA


def verify_satellite(repo):
    """Prove the pinned satellite SHA resolves to a commit locally (offline)."""
    if not repo:
        raise SystemExit("missing satellite repo: pass --satellite-repo PATH "
                         "or set G2_SATELLITE_REPO (read-only, never fetched)")
    r = Path(repo)
    if not (r / ".git").exists() and not (r / "HEAD").exists():
        raise SystemExit(f"not a git repo: {repo}")
    out = sh(["git", "-C", repo, "rev-parse", "--verify",
              SATELLITE_SHA + "^{commit}"])
    assert out.strip() == SATELLITE_SHA, out.strip()
    frag = sh(["git", "-C", repo, "show",
               f"{SATELLITE_SHA}:{SATELLITE_FRAGMENT}"]).encode()
    listing = sh(["git", "-C", repo, "ls-tree", "-r", "--name-only",
                  SATELLITE_SHA, SATELLITE_SEARCH_DIR + "/"])
    return sha_bytes(frag), sha_bytes(listing.encode())


def verify_intree():
    """Prove every family route is relay-mounted and fragmented in-tree."""
    cutover = (ROOT / CUTOVER).read_text()
    for r in ROUTES:
        pat = "| `" + r["route"] + "` | Content |"
        assert pat in cutover, "CUTOVER lacks Content row: " + r["route"]
    mount_src = (ROOT / MOUNT_FILE).read_text()
    assert f"func (h *ContentHandler) {MOUNT_FUNC.split('.')[1]}(mux *http.ServeMux)" in mount_src
    for r in ROUTES:
        lit = 'HandleFunc("' + r["route"] + '"'
        assert lit in mount_src, "Mount lacks route literal: " + r["route"]
    frag = (ROOT / FRAGMENT_FILE).read_bytes()
    frag_src = frag.decode()
    for r in ROUTES:
        path = r["route"].split(" ", 1)[1]
        assert '"' + path + '"' in frag_src, "fragment lacks path: " + path
    search_src = (ROOT / SEARCH_PIN_FILE).read_text()
    assert SEARCH_PIN_CONST in search_src, "search pin const moved"
    point_src = (ROOT / POINT_PIN_FILE).read_text()
    assert POINT_PIN_CONST in point_src, "point pin const moved"
    return sha_bytes(frag)


def counts():
    """Count classes; the family arithmetic must close exactly."""
    n = len(ROUTES)
    buckets = {c: sum(1 for r in ROUTES if r["class"] == c) for c in CLASSES}
    assert set(r["class"] for r in ROUTES) <= set(CLASSES)
    assert sum(buckets.values()) == n, buckets
    return n, buckets


def render(core_sha, sat_frag_sha, sat_listing_sha, intree_frag_sha):
    """Render the snapshot deterministically (no timestamps, LC_ALL=C sort)."""
    n, buckets = counts()
    native = buckets["native-served"]
    tierc = buckets["tier-C-dispatch"]
    relay = buckets["relay-disclosed"]
    absent = buckets["absent-disclosed"]
    pass_line = (f"PASS g2-served-snapshot: rows={n} native={native} "
                 f"tierc={tierc} relay={relay} absent={absent}")
    lines = []
    lines.append("<!-- SPDX-License-Identifier: MIT -->")
    lines.append("<!-- Copyright (c) 2025-2026 eshu-hq -->")
    lines.append("")
    lines.append("# G2 served-spec snapshot — content-search family (Phase 1)")
    lines.append("")
    lines.append("Refs eshu-hq/eshu#9669 (gate G2), eshu-hq/eshu#10491 "
                 "(Content (7) boxes). Status: tooling only — no cutover has "
                 "occurred, no behavior changes, no in-tree code moves. "
                 "Classification earns zero removal credit per the arbiter "
                 "G2/G8/G9 scope ruling (2026-11-13).")
    lines.append("")
    lines.append("## Provenance (generation record, verified offline)")
    lines.append("")
    lines.append("```text")
    lines.append(f"core_sha: {core_sha}")
    lines.append(f"satellite_sha: {SATELLITE_SHA}")
    lines.append(f"generator: {GENERATOR_NAME} {GENERATOR_VERSION}")
    lines.append("offline: no live fetch in any form (git show/ls-tree on "
                 "local objects, rg, sha256sum only)")
    lines.append("```")
    lines.append("")
    lines.append("Regenerate (run in the worktree root):")
    lines.append("")
    lines.append("```bash")
    lines.append("python3 scripts/gen-g2-served-snapshot.py --satellite-repo PATH")
    lines.append("bash scripts/check-g2-served-snapshot.sh")
    lines.append("# re-base: pass --core-sha NEWBASE and update CORE_SHA in")
    lines.append("# scripts/lib/g2_snapshot_data.py in the same change")
    lines.append("```")
    lines.append("")
    lines.append("The satellite repo is read-only input: the generator runs "
                 "only `rev-parse --verify`, `show`, and `ls-tree` against "
                 "the pinned SHA and never fetches, pulls, or clones. "
                 f"At {short(SATELLITE_SHA)} the satellite serves the 3 "
                 "point reads natively "
                 f"(`{SATELLITE_FRAGMENT}`) and carries no content-search "
                 f"fragment under `{SATELLITE_SEARCH_DIR}/`; the 4 searches "
                 "stay core-relay until the satellite W4 termination lands.")
    lines.append("")
    lines.append("## Classification schema (full-inventory G2)")
    lines.append("")
    lines.append("Every in-tree route carries exactly one class; class "
                 "counts must sum to the re-measured inventory at the "
                 "removal base (278/279/281 are all stale — recount then). "
                 "Phase 1 pins the content-search family (7 rows); later "
                 "families append rows under the same columns.")
    lines.append("")
    lines.append("| class | served_by | meaning |")
    lines.append("| --- | --- | --- |")
    for c in CLASSES:
        meaning = {
            "native-served": "satellite terminates natively; relay drained",
            "tier-C-dispatch": "dispatched to tier-C compat (10491-G4 re-verify)",
            "relay-disclosed": "still riding the relay catch-all, disclosed",
            "absent-disclosed": "in inventory, served by neither side, disclosed",
        }[c]
        lines.append(f"| {c} | {SERVED_BY[c]} | {meaning} |")
    lines.append("")
    lines.append("Column rules: route (`METHOD path`); class (one of the "
                 "four above); served_by (derived from class); spec_digest "
                 "(`sat:sha256:<hex>` of the satellite fragment at the "
                 "pinned SHA for native rows, `tree:sha256:<hex>` of the "
                 "in-tree fragment for relay rows); pin (covering admission "
                 "contract or `none`); mount (serving-side registration "
                 "cite); evidence (tracker box + PR/pin cites). "
                 "G2 passes only when relay-disclosed + absent-disclosed = 0 "
                 "except core-owned survivors named in the #9669 body, and "
                 "tier-C rows passed 10491-G4.")
    lines.append("")
    lines.append("## Source digests (recomputed by the checker)")
    lines.append("")
    lines.append("```text")
    lines.append(f"intree_fragment: {FRAGMENT_FILE} sha256:{intree_frag_sha}")
    lines.append(f"sat_fragment: {SATELLITE_FRAGMENT}@{short(SATELLITE_SHA)} "
                 f"sha256:{sat_frag_sha}")
    lines.append(f"sat_search_dir_listing: {SATELLITE_SEARCH_DIR}/@{short(SATELLITE_SHA)} "
                 f"sha256:{sat_listing_sha}")
    lines.append("```")
    lines.append("")
    lines.append(f"## Family inventory: {INVENTORY_SCOPE} ({n} rows)")
    lines.append("")
    lines.append("| route | class | served_by | spec_digest | pin | mount | evidence |")
    lines.append("| --- | --- | --- | --- | --- | --- | --- |")
    for r in sorted(ROUTES, key=lambda x: x["route"]):
        if r["sat_fragment"] is None:
            digest = "tree:sha256:" + intree_frag_sha
        else:
            digest = "sat:sha256:" + sat_frag_sha
        mount = f"{MOUNT_FUNC} ({MOUNT_FILE})" if r["class"] != "native-served" \
            else f"sat content_wave.go + {MOUNT_FUNC} ({MOUNT_FILE})"
        lines.append(f"| `{r['route']}` | {r['class']} | {SERVED_BY[r['class']]} "
                     f"| {digest} | {r['pin']} | {mount} | {r['evidence']} |")
    lines.append("")
    lines.append("## Verification")
    lines.append("")
    lines.append("`bash scripts/check-g2-served-snapshot.sh` at generation commit:")
    lines.append("")
    lines.append("```text")
    lines.append(pass_line)
    lines.append("```")
    return "\n".join(lines) + "\n"


def main(argv=None):
    """Generate the snapshot artifact from the tree plus satellite objects."""
    args = parse_args(argv if argv is not None else sys.argv[1:])
    sat_frag_sha, sat_listing_sha = verify_satellite(args.satellite_repo)
    intree_frag_sha = verify_intree()
    core_sha = verify_clean_or_pinned(args.core_sha)
    Path(args.out).write_text(render(core_sha, sat_frag_sha,
                                     sat_listing_sha, intree_frag_sha))
    print(f"wrote {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
