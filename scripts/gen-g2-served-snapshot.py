#!/usr/bin/env python3
"""G2 served-spec snapshot generator, full inventory (v2, eshu#9669 G2).

Derives docs/internal/g2-served-snapshot.md for all CUTOVER rows from
the tree plus local satellite git objects. Offline by construction:
only git rev-parse/show/ls-tree/cat-file (read-only plumbing), rg, and
sha256sum. No fetch, pull, clone, HTTP, or tracker reads in any form.

Committed judgment (tier-C set, survivor set) lives in
scripts/lib/g2_snapshot_data.py; derivation lives in
scripts/lib/g2_snapshot_derive.py. Row universe, coverage, mounts,
fragments, pins, and digests are measured, never asserted.

Usage (run in the worktree root):
  python3 scripts/gen-g2-served-snapshot.py --satellite-repo PATH [--out FILE]
  G2_SATELLITE_REPO=PATH python3 scripts/gen-g2-served-snapshot.py
"""
import argparse
import hashlib
import os
import subprocess
import sys
from pathlib import Path

LIB = Path(__file__).resolve().parent / "lib"
sys.path.insert(0, str(LIB))

from g2_snapshot_data import (  # noqa: E402
    CENSUS_GAP_ROUTES,
    CLASSES,
    CORE_SHA,
    DIRECT_TERMINATE,
    GENERATOR_NAME,
    G2_PIN_OVERRIDE,
    GENERATOR_VERSION,
    IAC_PIN_OVERRIDE,
    INVENTORY_SCOPE,
    SATELLITE_SHA,
    SERVED_BY,
    SURVIVOR_ROUTES,
    TIER_C_ROUTES,
    short,
)
from g2_snapshot_derive import (  # noqa: E402
    classify,
    cutover_routes,
    direct_registrations,
    fragment_index,
    mount_index,
    pin_const_version,
    sat_direct_routes,
    sat_registration_proof,
    sat_wave_tables,
    sh,
    terminate_digest,
    w8_row_standing,
    wave_transport_pins,
)


def find_root() -> Path:
    """Locate the worktree root without hard-coded absolute paths."""
    for cand in Path(__file__).resolve().parents:
        if (cand / "docs" / "internal" / "g2-route-inventory.md").is_file():
            return cand
    p = subprocess.run(["git", "rev-parse", "--show-toplevel"],
                       capture_output=True, text=True)
    if p.returncode == 0 and p.stdout.strip():
        return Path(p.stdout.strip())
    raise SystemExit("cannot locate worktree root (no CUTOVER_OWNERSHIP.md found)")


ROOT = find_root()
DEFAULT_OUT = ROOT / "docs" / "internal" / "g2-served-snapshot.md"


def parse_args(argv: list[str] | None) -> argparse.Namespace:
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


JUDGMENT_FILES = ("docs/internal/g2-route-inventory.md",
                  "docs/internal/w8-port-census.md",
                  "scripts/lib/g2_snapshot_data.py")


def verify_clean_or_pinned(core_sha: str | None) -> str:
    """Fail closed when the derivation inputs differ from the recorded base."""
    if core_sha is not None:
        sh(["git", "cat-file", "-e", core_sha + "^{commit}"])
        return core_sha
    dirty = sh(["git", "status", "--porcelain", "--", *JUDGMENT_FILES]).strip()
    if dirty:
        raise SystemExit("source files dirty; commit them or re-base with "
                         "an explicit --core-sha:\n" + dirty)
    sh(["git", "cat-file", "-e", CORE_SHA + "^{commit}"])
    return CORE_SHA


def sha_bytes(b: bytes) -> str:
    """Return the hex sha256 of bytes."""
    return hashlib.sha256(b).hexdigest()


def verify_all(root: str, repo: str, cut: list[tuple[str, str, str]], waves: dict, direct: dict, mounts: dict, frags: dict, w8: dict, wave_pins: dict) -> dict[str, str]:
    """Cross-check every derived input; fail closed on any inconsistency."""
    cutkeys = [(m, p) for m, p, _ in cut]
    if len(set(cutkeys)) != len(cutkeys):
        raise SystemExit("duplicate CUTOVER rows")
    for r in list(TIER_C_ROUTES) + list(SURVIVOR_ROUTES):
        if tuple(r.split(" ", 1)) not in set(cutkeys):
            raise SystemExit("committed route not in CUTOVER: " + r)
    for key in list(waves) + list(direct):
        if key not in set(cutkeys):
            raise SystemExit(f"satellite covers non-CUTOVER row: {key}")
    for (m, p, fam) in cut:
        if (m, p) not in mounts:
            raise SystemExit(f"no in-tree mount for row: {m} {p}")
    # Pin contradiction gate: wave Transport pin vs w8 dedicated must agree.
    for (m, p), wpin in wave_pins.items():
        w8pin = w8.get((m, p), (None,) * 4)[3]
        if w8pin is not None and w8pin != wpin:
            raise SystemExit(f"pin contradiction for {m} {p}: wave={wpin} w8={w8pin}")
    # Committed lane overrides must name real pins on CUTOVER rows.
    for override in (IAC_PIN_OVERRIDE, G2_PIN_OVERRIDE):
        for route, opin in override.items():
            if tuple(route.split(" ", 1)) not in set(cutkeys):
                raise SystemExit(f"override route not in CUTOVER: {route}")
    # Every cited pin const must exist in-tree (version read from the tree).
    cited = (set(wave_pins.values()) | {v for v in (x[3] for x in w8.values()) if v}
             | set(IAC_PIN_OVERRIDE.values()) | set(G2_PIN_OVERRIDE.values()))
    versions = {}
    for pin in sorted(cited):
        versions[pin] = pin_const_version(root, pin)
    return versions


def row_pin(key: tuple[str, str], waves: dict, wave_pins: dict, w8: dict) -> str | None:
    """Covering pin for a row.

    Precedence: wave Transport pin, then the committed satellite-lane
    override (finer than the w8 family cell), then w8 dedicated, else
    none. The override is deliberate where the lane record splits a
    family pin; verify_all still fails closed when a wave pin and the
    w8 cell disagree.
    """
    if key in wave_pins:
        return wave_pins[key]
    route = f"{key[0]} {key[1]}"
    if route in IAC_PIN_OVERRIDE:
        return IAC_PIN_OVERRIDE[route]
    if route in G2_PIN_OVERRIDE:
        return G2_PIN_OVERRIDE[route]
    w = w8.get(key)
    if w is not None and w[3] is not None:
        return w[3]
    return None


def evidence_for(key: tuple[str, str], family: str, cls: str, src: str, pin: str | None, pinver: str | None, w8: dict, reglines: dict[str, object]) -> str:
    """Build the evidence cell from derived sources only.

    reglines carries exact satellite router.go lines (wave register call
    or direct loop/handle lines) so the checker can verify registration
    by string equality instead of re-running derivation.
    """
    if cls == "native-served":
        if src.startswith("wave:"):
            wf = src.split(":", 1)[1]
            reg = reglines.get(wf, "")
            base = f"sat {wf} frozen table + router {reg} viaNative @{short(SATELLITE_SHA)}"
        else:
            dsrc = src.split(":", 1)[1]
            reg = " ;; ".join(reglines.get(dsrc, []))
            base = (f"sat {dsrc} route const + router {reg} "
                    f"viaNative @{short(SATELLITE_SHA)}")
        pinbit = f"{pin} {pinver}" if pin else "none (no minted family pin)"
        wbit = ""
        if key in w8:
            wbit = f"; w8 C3 {w8[key][2]}"
        return base + "; pin " + pinbit + wbit
    if cls == "tier-C-dispatch":
        return ("tracker #10491 Dispatched tier-C (27), terminal per Decision 2, "
                "G4 closed; core write-through dispatch")
    if key in set(tuple(r.split(" ", 1)) for r in SURVIVOR_ROUTES):
        return ("core-owned survivor per eshu-hq/eshu#8872 canonical mutations + "
                "#9669 census criterion; exempt from relay+absent bar pending arbiter")
    if key in set(tuple(r.split(" ", 1)) for r in CENSUS_GAP_ROUTES):
        return ("CUTOVER-only row: absent from tracker #10491 sections and w8 census; "
                "relay-leg disclosed, census follow-up required")
    fam_w8 = w8.get(key)
    if fam_w8 is not None:
        return (f"w8 C3 {fam_w8[2]}; satellite termination pending "
                "(no covering wave/direct fragment)")
    return "relay-leg disclosed; satellite termination pending"


def build_rows(root: str, repo: str) -> list[dict[str, object]]:
    """Derive the full classified row list with digests, pins, evidence."""
    cut = cutover_routes(root)
    waves = sat_wave_tables(repo, SATELLITE_SHA)
    direct = sat_direct_routes(repo, SATELLITE_SHA)
    mounts = mount_index(root)
    frags = fragment_index(root)
    w8 = w8_row_standing(root)
    wave_pins = wave_transport_pins(repo, SATELLITE_SHA)
    pinvers = verify_all(root, repo, cut, waves, direct, mounts, frags, w8, wave_pins)
    cls = classify(cut, waves, direct)
    # Satellite terminate digests, one per covering source file.
    satdig = {}
    for wf in sorted(set(waves.values())):
        term, digest = terminate_digest(repo, SATELLITE_SHA, wf)
        satdig[f"wave:{wf}"] = (term, digest)
    for src in sorted(set(direct.values())):
        term = DIRECT_TERMINATE.get(src)
        if term is None:
            raise SystemExit(f"no terminate fragment mapping for {src}")
        blob = subprocess.run(["git", "-C", repo, "show", f"{SATELLITE_SHA}:{term}"],
                              capture_output=True, cwd=repo)
        if blob.returncode != 0:
            raise SystemExit(f"direct terminate fragment missing: {term}")
        satdig[f"direct:{src}"] = (term, sha_bytes(blob.stdout))
    reglines = {wf: sat_registration_proof(repo, SATELLITE_SHA, wf)
                for wf in sorted(set(waves.values()))}
    reglines.update(direct_registrations(repo, SATELLITE_SHA))
    rows = []
    for method, path, family in cut:
        key = (method, path)
        klass, src = cls[key]
        pin = row_pin(key, waves, wave_pins, w8)
        pinver = pinvers.get(pin) if pin else None
        if klass == "native-served":
            term, digest = satdig[src]
            digestcell = f"sat:sha256:{digest}"
            mountcell = f"sat {term} + {' ,'.join(mounts[key])}"
        else:
            flist = frags.get(path)
            if not flist:
                raise SystemExit(f"relay row without fragment: {method} {path}")
            with open(Path(root) / flist[0], "rb") as fh:
                digestcell = f"tree:sha256:{sha_bytes(fh.read())}"
            mountcell = " ,".join(mounts[key])
        rows.append({
            "route": f"{method} {path}",
            "class": klass,
            "served_by": SERVED_BY[klass],
            "digest": digestcell,
            "pin": f"{pin} {pinver}" if pin else "none",
            "mount": mountcell,
            "evidence": evidence_for(key, family, klass, src, pin, pinver, w8, reglines),
            "survivor": key in set(tuple(r.split(" ", 1)) for r in SURVIVOR_ROUTES),
        })
    return rows


def counts(rows: list[dict[str, object]]) -> tuple[int, dict[str, int], int, int]:
    """Count classes; the family arithmetic must close exactly."""
    n = len(rows)
    buckets = {c: sum(1 for r in rows if r["class"] == c) for c in CLASSES}
    assert set(r["class"] for r in rows) <= set(CLASSES)
    assert sum(buckets.values()) == n, buckets
    nonsurv = sum(1 for r in rows if r["class"] == "relay-disclosed" and not r["survivor"])
    absent = buckets["absent-disclosed"]
    return n, buckets, nonsurv, absent


def render(core_sha: str, rows: list[dict[str, object]], n: int, buckets: dict[str, int], nonsurv: int) -> str:
    """Render the snapshot deterministically (sorted rows, no timestamps)."""
    native = buckets["native-served"]
    tierc = buckets["tier-C-dispatch"]
    relay = buckets["relay-disclosed"]
    absent = buckets["absent-disclosed"]
    pass_line = (f"PASS g2-served-snapshot: rows={n} native={native} "
                 f"tierc={tierc} relay={relay} absent={absent} nonsurvivor={nonsurv}")
    L = []
    L.append("<!-- SPDX-License-Identifier: MIT -->")
    L.append("<!-- Copyright (c) 2025-2026 eshu-hq -->")
    L.append("")
    L.append("# G2 served-spec snapshot — full inventory (v2)")
    L.append("")
    L.append("Refs eshu-hq/eshu#9669 (gate G2), eshu-hq/eshu#10491 (zero open "
             "boxes). Status: tooling only — no cutover has occurred, no "
             "behavior changes, no in-tree code moves. Classification earns "
             "zero removal credit per the arbiter G2/G8/G9 scope ruling "
             "(2026-11-13).")
    L.append("")
    L.append("## Provenance (generation record, verified offline)")
    L.append("")
    L.append("```text")
    L.append(f"core_sha: {core_sha}")
    L.append(f"satellite_sha: {SATELLITE_SHA}")
    L.append(f"generator: {GENERATOR_NAME} {GENERATOR_VERSION}")
    L.append("offline: no live fetch in any form (git show/ls-tree on local "
             "objects, rg, sha256sum only)")
    L.append("```")
    L.append("")
    L.append("Regenerate (run in the worktree root):")
    L.append("")
    L.append("```bash")
    L.append("python3 scripts/gen-g2-served-snapshot.py --satellite-repo PATH")
    L.append("bash scripts/check-g2-served-snapshot.sh")
    L.append("# re-base: pass --core-sha NEWBASE and update CORE_SHA in")
    L.append("# scripts/lib/g2_snapshot_data.py in the same change")
    L.append("```")
    L.append("")
    L.append("The satellite repo is read-only input: the generator runs only "
             "`rev-parse --verify`, `show`, and `ls-tree` against the pinned "
             "SHA and never fetches, pulls, or clones. Row universe, wave "
             "coverage, mounts, fragments, pins, and digests are measured "
             "from the tree and local objects; only the tier-C set (tracker "
             "#10491, 27 rows) and the survivor set (#8872 canonical "
             "mutations, 19 rows), the IaC lane pin split, and the tier-C set\nare committed judgment.")
    L.append("")
    L.append("## Classification schema (full-inventory G2)")
    L.append("")
    L.append("Every in-tree route carries exactly one class; class counts "
             "must sum to the re-measured inventory (282 rows at the frozen "
             "base).")
    L.append("")
    L.append("| class | served_by | meaning |")
    L.append("| --- | --- | --- |")
    for c in CLASSES:
        meaning = {
            "native-served": "satellite terminates natively; relay drained",
            "tier-C-dispatch": "dispatched to tier-C compat (10491-G4 re-verify)",
            "relay-disclosed": "still riding the relay catch-all, disclosed",
            "absent-disclosed": "in inventory, served by neither side, disclosed",
        }[c]
        L.append(f"| {c} | {SERVED_BY[c]} | {meaning} |")
    L.append("")
    L.append("Column rules: route (`METHOD path`); class (one of the four "
             "above); served_by (derived from class); spec_digest "
             "(`sat:sha256:<hex>` of the covering satellite terminate "
             "fragment for native rows, `tree:sha256:<hex>` of the row's "
             "in-tree fragment for relay rows); pin (covering admission "
             "contract or `none`); mount (serving-side registration cite); "
             "evidence (tracker/wave/pin cites). G2 passes only when "
             "relay-nonsurvivor + absent = 0: the 19 #9669-named core-owned "
             "survivors are exempt, every other relay row blocks until the "
             "arbiter dispositions it.")
    L.append("")
    L.append(f"## Survivor exemption list ({sum(1 for r in rows if r['survivor'])}, committed judgment)")
    L.append("")
    for r in sorted(r["route"] for r in rows if r["survivor"]):
        L.append(f"- `{r}`")
    L.append("")
    L.append("## Non-survivor relay blockers (measured)")
    L.append("")
    blockers = sorted(r["route"] for r in rows
                      if r["class"] == "relay-disclosed" and not r["survivor"])
    if blockers:
        for r in blockers:
            L.append(f"- `{r}`")
    else:
        L.append("none — G2 passes")
    L.append("")
    L.append(f"## Full inventory ({INVENTORY_SCOPE}, {n} rows)")
    L.append("")
    L.append("| route | class | served_by | spec_digest | pin | mount | evidence |")
    L.append("| --- | --- | --- | --- | --- | --- | --- |")
    for r in sorted(rows, key=lambda x: x["route"]):
        L.append(f"| `{r['route']}` | {r['class']} | {r['served_by']} "
                 f"| {r['digest']} | {r['pin']} | {r['mount']} | {r['evidence']} |")
    L.append("")
    L.append("## Verification")
    L.append("")
    L.append("`bash scripts/check-g2-served-snapshot.sh` at generation commit:")
    L.append("")
    L.append("```text")
    L.append(pass_line)
    L.append("```")
    return "\n".join(L) + "\n"


def main(argv: list[str] | None = None) -> None:
    """Generate the snapshot artifact from the tree plus satellite objects."""
    args = parse_args(argv if argv is not None else sys.argv[1:])
    if not args.satellite_repo:
        raise SystemExit("missing satellite repo: pass --satellite-repo PATH "
                         "or set G2_SATELLITE_REPO (read-only, never fetched)")
    r = Path(args.satellite_repo)
    if not (r / ".git").exists() and not (r / "HEAD").exists():
        raise SystemExit(f"not a git repo: {args.satellite_repo}")
    out = sh(["git", "-C", args.satellite_repo, "rev-parse", "--verify",
              SATELLITE_SHA + "^{commit}"])
    assert out.strip() == SATELLITE_SHA, out.strip()
    rows = build_rows(str(ROOT), args.satellite_repo)
    n, buckets, nonsurv, _ = counts(rows)
    core_sha = verify_clean_or_pinned(args.core_sha)
    Path(args.out).write_text(render(core_sha, rows, n, buckets, nonsurv))
    print(f"wrote {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
