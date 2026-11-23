#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Offline G2 gate checks, extracted from check-g2-served-snapshot.sh.

Bash heredocs over 512 bytes trip the heredoc-budget guard (bash 5.1+
pipe-buffer deadlock), so the checker's Python probes live here as
subcommands instead of inline heredocs. Behavior is byte-identical to the
heredocs they replace; the checker shell still owns thresholds, files,
and exit codes.

Subcommands (all read-only; CWD must be the worktree root, same as the
checker shell):
  derive-nonsurvivor SNAPSHOT   print export G2_EXPECT_NONSURVIVOR=...
  survivor-check SNAPSHOT       print non-survivor rows to stdout
  digest-check SNAPSHOT         print digest drifts to stdout
  satellite-check SNAPSHOT REPO SHA
                                verify satellite digests/coverage via
                                read-only `git -C REPO show`
"""

from __future__ import annotations

import hashlib
import os
import pathlib
import re
import subprocess
import sys


def _lib_on_path() -> None:
    here = os.path.dirname(os.path.abspath(__file__))
    if here not in sys.path:
        sys.path.insert(0, here)


_lib_on_path()

from g2_snapshot_data import (  # noqa: E402  (path fixed above)
    DIRECT_TERMINATE,
    SURVIVOR_ROUTES,
    TIER_C_ROUTES,
    WAVE_TERMINATE_OVERRIDE,
)
from g2_snapshot_derive import fragment_index  # noqa: E402


def cmd_derive_nonsurvivor(snapshot: str) -> int:
    sn = open(snapshot).read()
    relay = set(
        re.findall(
            r"^\| `(GET|POST|PATCH|PUT|DELETE) [^`]+` \| relay-disclosed \|",
            sn,
            re.M,
        )
    )
    surv: set[str] = set()
    m = re.search(r"## Survivor exemption list.*?\n\n(.*?)\n\n## ", sn, re.S)
    if m:
        surv = set(re.findall(r"^- `([^`]+)`", m.group(1), re.M))
    print(f"export G2_EXPECT_NONSURVIVOR={len(relay - surv)}")
    return 0


def cmd_survivor_check(snapshot: str) -> int:
    sn = open(snapshot).read()
    rowclass = {}
    for m in re.finditer(
        r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| (\S+) \|", sn, re.M
    ):
        rowclass[f"{m.group(1)} {m.group(2)}"] = m.group(3)
    relay = {r for r, c in rowclass.items() if c == "relay-disclosed"}
    tierc = {r for r, c in rowclass.items() if c == "tier-C-dispatch"}
    surv = set(SURVIVOR_ROUTES)
    if not surv <= relay:
        print(f"survivor outside relay: {sorted(surv - relay)}")
        return 1
    if tierc != set(TIER_C_ROUTES):
        print(
            "tier-C drift: "
            f"extra={sorted(tierc - set(TIER_C_ROUTES))} "
            f"missing={sorted(set(TIER_C_ROUTES) - tierc)}"
        )
        return 1
    for r in sorted(relay - surv):
        print(r)
    return 0


def cmd_digest_check(snapshot: str) -> int:
    sn = open(snapshot).read()
    frags = fragment_index(".")
    bad = []
    for m in re.finditer(
        r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| "
        r"(?:relay-disclosed \| core-relay|tier-C-dispatch \| tier-C) \| "
        r"tree:sha256:([0-9a-f]{64}) \|",
        sn,
        re.M,
    ):
        method, path, want = m.group(1), m.group(2), m.group(3)
        flist = frags.get(path)
        if not flist:
            bad.append(f"{method} {path}: no in-tree fragment")
            continue
        got = hashlib.sha256(pathlib.Path(flist[0]).read_bytes()).hexdigest()
        if got != want:
            bad.append(f"{method} {path}: digest drift in {flist[0]}")
    if bad:
        print("\n".join(bad))
        return 1
    return 0


def cmd_satellite_check(snapshot: str, repo: str, sha: str) -> int:
    def show(path: str) -> bytes:
        p = subprocess.run(
            ["git", "-C", repo, "show", f"{sha}:{path}"],
            capture_output=True,
        )
        if p.returncode != 0:
            print(f"satellite blob missing: {path}")
            sys.exit(1)
        return p.stdout

    sn = open(snapshot).read()
    router = show("api/router.go").decode()
    bad = []
    row_pat = re.compile(
        r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| native-served \| "
        r"satellite-native \| sat:sha256:([0-9a-f]{64}) \|"
    )
    for line in sn.splitlines():
        m = row_pat.match(line)
        if not m:
            continue
        method, path, want = m.group(1), m.group(2), m.group(3)
        ev_cells = [c for c in line.split("|") if "viaNative" in c]
        if not ev_cells:
            bad.append(f"{method} {path}: no sat evidence cell")
            continue
        sm = re.search(r"sat (\S+) .*?router (.*?) viaNative @", ev_cells[0])
        if not sm:
            bad.append(f"{method} {path}: unparseable sat evidence")
            continue
        src = sm.group(1)
        if src in WAVE_TERMINATE_OVERRIDE:
            term = WAVE_TERMINATE_OVERRIDE[src]
        elif src.endswith("_wave.go"):
            term = "api/" + pathlib.Path(src).name.replace(
                "_wave.go", "_terminate.go"
            )
        elif src in DIRECT_TERMINATE:
            term = DIRECT_TERMINATE[src]
        else:
            bad.append(f"{method} {path}: unknown sat source {src}")
            continue
        got = hashlib.sha256(show(term)).hexdigest()
        if got != want:
            bad.append(f"{method} {path}: satellite digest drift in {term}")
            continue
        body = show(src).decode(errors="replace")
        if path not in body:
            bad.append(f"{method} {path}: not covered by {src}")
            continue
        for rl in sm.group(2).split(" ;; "):
            rl = rl.strip()
            if rl and rl not in router:
                bad.append(
                    f"{method} {path}: registration absent from router.go: "
                    f"{rl[:80]}"
                )
    if bad:
        print("\n".join(bad))
        return 1
    return 0


_COMMANDS = {
    "derive-nonsurvivor": (cmd_derive_nonsurvivor, 1),
    "survivor-check": (cmd_survivor_check, 1),
    "digest-check": (cmd_digest_check, 1),
    "satellite-check": (cmd_satellite_check, 3),
}


def main(argv: list[str]) -> int:
    if len(argv) < 2 or argv[1] not in _COMMANDS:
        print(
            "usage: g2_snapshot_gate.py "
            "{derive-nonsurvivor|survivor-check|digest-check|satellite-check} "
            "SNAPSHOT [REPO SHA]",
            file=sys.stderr,
        )
        return 2
    fn, want = _COMMANDS[argv[1]]
    if len(argv[2:]) != want:
        print(f"{argv[1]} takes {want} argument(s)", file=sys.stderr)
        return 2
    return fn(*argv[2:])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
