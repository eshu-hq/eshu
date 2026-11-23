"""Derivation helpers for the G2 served-spec snapshot (v2, eshu#9669).

Every function derives from the tree or from local satellite git objects.
No network, no fetch/pull/clone. Shared by the generator (which renders
the snapshot) and the checker (which re-derives expectations). The only
committed judgment inputs are TIER_C_ROUTES and SURVIVOR_ROUTES in
g2_snapshot_data; everything else is measured.
"""

import argparse
import hashlib
import re
import subprocess
import sys
from pathlib import Path

LIB = Path(__file__).resolve().parent
sys.path.insert(0, str(LIB))

from g2_snapshot_data import (  # noqa: E402
    CUTOVER,
    D9_DIRECT_FILES,
    IAC_FILES,
    SATELLITE_SHA,
    SETUP_FILES,
    SURVIVOR_ROUTES,
    TIER_C_ROUTES,
    W8_CENSUS,
    WAVE_TERMINATE_OVERRIDE,
)

type RouteKey = tuple[str, str]
"""A (METHOD, path) route identity, e.g. ("GET", "/api/v0/catalog")."""


def sh(args: list[str], cwd: str | Path | None = None) -> str:
    """Run a command, failing closed with stderr on error.

    Args:
        args: Command and arguments to run.
        cwd: Working directory, defaults to the caller process directory.

    Returns:
        The command's stdout.

    Raises:
        SystemExit: When the command exits nonzero, with stderr attached.
    """
    p = subprocess.run(args, capture_output=True, text=True,
                       cwd=str(cwd) if cwd else None)
    if p.returncode != 0:
        raise SystemExit(f"command failed: {' '.join(args)}\n{p.stderr.strip()[:500]}")
    return p.stdout


def cutover_routes(root: str | Path) -> list[tuple[str, str, str]]:
    """Parse the CUTOVER route table.

    Args:
        root: Worktree root holding the vendored CUTOVER route table
            (docs/internal/g2-route-inventory.md).

    Returns:
        (method, path, family) triples in file order.
    """
    pat = re.compile(r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| ([^ |]+) \|")
    rows = []
    for line in (Path(root) / CUTOVER).read_text().splitlines():
        m = pat.match(line)
        if m:
            rows.append((m.group(1), m.group(2), m.group(3)))
    return rows


def _literal_hits(root: str | Path, pattern: str, *scopes: str) -> dict[RouteKey, list[str]]:
    """Map (method, path) literal hits to sorted file lists.

    Searches non-test .go files under the given scopes for quoted
    `"METHOD /path"` literals.

    Args:
        root: Worktree root.
        pattern: rg pattern for quoted route literals.
        scopes: Tree-relative directories to search.

    Returns:
        Mapping of route keys to sorted lists of files containing them.
    """
    out = sh(["rg", "--no-heading", "-o", "-N", "--glob", "!*_test.go",
              "--glob", "*.go", pattern, *scopes], cwd=root)
    idx: dict[RouteKey, set[str]] = {}
    for line in out.splitlines():
        f, _, lit = line.partition(":")
        lit = lit.strip().strip('"')
        if " " not in lit:
            continue
        method, path = lit.split(" ", 1)
        idx.setdefault((method, path), set()).add(f)
    return {k: sorted(v) for k, v in idx.items()}


def mount_index(root: str | Path) -> dict[RouteKey, list[str]]:
    """Map every in-tree route literal to its mount files.

    Args:
        root: Worktree root.

    Returns:
        Mapping of route keys to sorted mount file lists.
    """
    # Post-#10890 the relay/compat serving tree (go/cmd/api) is deleted;
    # mounts resolve via the remaining go/internal/query registrations.
    return _literal_hits(root, r'"(GET|POST|PATCH|PUT|DELETE) /[a-z0-9][^"]*"',
                         "go/internal")


def fragment_index(root: str | Path) -> dict[str, list[str]]:
    """Map openapi fragment path literals to fragment files.

    Args:
        root: Worktree root.

    Returns:
        Mapping of route paths to sorted fragment file lists.
    """
    out = sh(["rg", "--no-heading", "-o", "-N", "--glob", "!*_test.go",
              "--glob", "*.go", r'"/api/v[0-9][^"]*"',
              "go/internal/query/openapi"], cwd=root)
    idx: dict[str, set[str]] = {}
    for line in out.splitlines():
        f, _, lit = line.partition(":")
        idx.setdefault(lit.strip().strip('"'), set()).add(f)
    return {k: sorted(v) for k, v in idx.items()}


def _sat_show(repo: str, sha: str, path: str) -> str:
    """Show a satellite blob as text (read-only plumbing).

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.
        path: Tree-relative blob path.

    Returns:
        The blob decoded as text.

    Raises:
        SystemExit: When git show fails.
    """
    p = subprocess.run(["git", "-C", repo, "show", f"{sha}:{path}"],
                       capture_output=True, text=True)
    if p.returncode != 0:
        raise SystemExit(f"satellite blob missing: {sha[:12]}:{path}")
    return p.stdout


def sat_wave_tables(repo: str, sha: str) -> dict[RouteKey, str]:
    """Extract frozen wave tables: {(method, path): wavefile}.

    Resolves `Path: <ident>` through the file's own const block.

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.

    Returns:
        Mapping of route keys to owning wave files.

    Raises:
        SystemExit: On unresolvable idents or duplicate rows.
    """
    files = sh(["git", "-C", repo, "ls-tree", "-r", "--name-only",
                sha, "--", "api/"]).split()
    waves = sorted(f for f in files if f.endswith("_wave.go") and "test" not in f)
    tables = {}
    for wf in waves:
        body = _sat_show(repo, sha, wf)
        consts = dict(re.findall(r"^\s*(\w+)\s*=\s*\"(/[^\"]+)\"", body, re.M))
        for m in re.finditer(r"\{Method:\s*\"([A-Z]+)\",\s*Path:\s*(\"[^\"]+\"|\w+)", body):
            method, raw = m.groups()
            if raw.startswith('"'):
                path = raw.strip('"')
            else:
                if raw not in consts:
                    raise SystemExit(f"unresolvable Path ident {raw} in {wf}")
                path = consts[raw]
            if (method, path) in tables:
                raise SystemExit(f"duplicate wave row {method} {path}: {tables[(method, path)]} vs {wf}")
            tables[(method, path)] = wf
    return tables


def sat_direct_routes(repo: str, sha: str) -> dict[RouteKey, str]:
    """Extract direct-termination route consts (d9 logins, setup).

    Reads the `METHOD path` consts from api/d9.go, api/d9github.go,
    api/d9saml.go and api/setup.go.

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.

    Returns:
        Mapping of route keys to the const-defining source file.
    """
    direct = {}
    for f in D9_DIRECT_FILES + IAC_FILES:
        body = _sat_show(repo, sha, f)
        for m in re.finditer(r"^\s*\w+\s*=\s*\"((GET|POST|PATCH|PUT|DELETE) /[^\"]+)\"", body, re.M):
            method, path = m.group(1).split(" ", 1)
            direct[(method, path)] = f
    for f in SETUP_FILES:
        try:
            body = _sat_show(repo, sha, f)
        except SystemExit:
            continue
        for m in re.finditer(r"^\s*\w+\s*=\s*\"((GET|POST|PATCH|PUT|DELETE) /[^\"]+)\"", body, re.M):
            method, path = m.group(1).split(" ", 1)
            direct.setdefault((method, path), f)
    return direct


def sat_registration_proof(repo: str, sha: str, wavefile: str) -> str:
    """Return the router registration line for a wave file.

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.
        wavefile: Wave file, e.g. api/content_search_wave.go.

    Returns:
        The exact stripped router.go registration line.

    Raises:
        SystemExit: When no registration line names the wave stem.
    """
    body = _sat_show(repo, sha, wavefile)
    tables = re.findall(r"^var\s+(\w+)\s*=\s*\[\]\w*Route\{", body, re.M)
    router = _sat_show(repo, sha, "api/router.go")
    for line in router.splitlines():
        s = line.strip()
        if any(t and t in s for t in tables):
            return s
    norm = Path(wavefile).stem.replace("_wave", "").replace("_", "").lower()
    for line in router.splitlines():
        squashed = line.replace("_", "").lower()
        if norm in squashed and "register" in squashed:
            return line.strip()
    raise SystemExit(f"no router registration for {wavefile} at {sha[:12]}")


def direct_registrations(repo: str, sha: str) -> dict[str, list[str]]:
    """Collect router registration lines for direct (non-wave) routes.

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.

    Returns:
        Mapping of source file to exact router.go lines covering it.

    Raises:
        SystemExit: When any expected line is absent.
    """
    router = _sat_show(repo, sha, "api/router.go")
    want = {
        "api/d9.go": ("d9Routes", "oidcTerminatedHandler"),
        "api/d9github.go": ("d9GitHubRoutes", "githubTerminatedHandler"),
        "api/d9saml.go": ("d9SAMLRoutes", "samlTerminatedHandler"),
        "api/setup.go": ("setupStateRoute", "setupWriteThrough"),
        "api/iac.go": ("iacNativeRoutes", "iacWriteThrough"),
    }
    found = {}
    for src, (loop, handler) in want.items():
        lines = [ln.strip() for ln in router.splitlines()
                 if loop in ln or handler in ln]
        if not lines:
            raise SystemExit(f"no router registration for {src} at {sha[:12]}")
        found[src] = lines
    return found


def w8_row_standing(root: str | Path) -> dict[RouteKey, tuple]:
    """Parse the w8 census route table.

    Args:
        root: Worktree root holding docs/internal/w8-port-census.md.

    Returns:
        Mapping of route keys to (family, c1, c2, c3, pin|None) where
        pin is the C2 `dedicated:X` name when present.
    """
    rows = {}
    pat = re.compile(r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| ([^|]+) \| ([^|]+) \| ([^|]+) \|")
    for line in (Path(root) / W8_CENSUS).read_text().splitlines():
        m = pat.match(line)
        if not m:
            continue
        method, path = m.group(1), m.group(2)
        fam, c1, c2 = m.group(3).strip(), m.group(4).strip(), m.group(5).strip()
        dm = re.match(r"dedicated:(\w+)", c2)
        rows[(method, path)] = (fam, c1, c2, dm.group(1) if dm else None)
    # NOTE: the published census table carries route|family|C1|C2|C3|C4|C5;
    # this parser keeps family + the C1..C3 cells and the dedicated pin.
    return rows


def wave_transport_pins(repo: str, sha: str) -> dict[RouteKey, str]:
    """Map wave rows to Transport-pin names: {(method,path): pin}.

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.

    Returns:
        Mapping of wave-covered route keys to pin names.

    Raises:
        SystemExit: When two waves claim one row with different pins.
    """
    tables = sat_wave_tables(repo, sha)
    by_wave: dict[str, list[RouteKey]] = {}
    for rp, wf in tables.items():
        by_wave.setdefault(wf, []).append(rp)
    pins = {}
    for wf, rps in by_wave.items():
        body = _sat_show(repo, sha, wf)
        m = re.search(r"Transport pin:\s*([A-Za-z]+)\s+v[0-9]+", body)
        if not m:
            continue
        for rp in rps:
            if rp in pins and pins[rp] != m.group(1):
                raise SystemExit(f"conflicting pins for {rp}: {pins[rp]} vs {m.group(1)}")
            pins[rp] = m.group(1)
    return pins


def pin_const_version(root: str | Path, pin: str) -> str:
    """Return the in-tree version of a pin const.

    Args:
        root: Worktree root.
        pin: Pin base name, e.g. ContentSearchReadAdmission.

    Returns:
        The version string from `<pin>ContractVersion = "vX"`.

    Raises:
        SystemExit: When the const is missing or version-conflicted.
    """
    const = pin if pin.endswith("Version") else f"{pin}ContractVersion"
    out = sh(["rg", "--no-heading", "-o", "-N",
              re.escape(f"{const} = ") + r'"[^"]+"', "go"], cwd=root)
    vers = set()
    for line in out.splitlines():
        _, _, lit = line.partition(":")
        m = re.search(r'"([^"]+)"', lit)
        if m:
            vers.add(m.group(1))
    if not vers:
        raise SystemExit(f"pin const missing in-tree: {pin}")
    if len(vers) != 1:
        raise SystemExit(f"pin const version conflict: {pin} -> {sorted(vers)}")
    return vers.pop()


def terminate_digest(repo: str, sha: str, wavefile: str) -> tuple[str, str]:
    """Digest the *_terminate.go blob owning a wave file.

    Uses WAVE_TERMINATE_OVERRIDE when the wave is served by another
    fragment (d3trio via d3_terminate), else the mechanical sibling.

    Args:
        repo: Satellite checkout path.
        sha: Pinned commit SHA.
        wavefile: Wave file, e.g. api/entity_wave.go.

    Returns:
        (terminate path, hex sha256) of the owning blob.

    Raises:
        SystemExit: When the owning blob is absent.
    """
    if wavefile in WAVE_TERMINATE_OVERRIDE:
        term = WAVE_TERMINATE_OVERRIDE[wavefile]
    else:
        term = str(Path(wavefile).parent / (Path(wavefile).stem.replace("_wave", "_terminate") + ".go"))
    blob = subprocess.run(["git", "-C", repo, "show", f"{sha}:{term}"],
                          capture_output=True, cwd=repo)
    if blob.returncode != 0:
        raise SystemExit(f"terminate fragment missing for {wavefile}: {term}")
    return term, hashlib.sha256(blob.stdout).hexdigest()


def classify(
    cut: list[tuple[str, str, str]],
    waves: dict[RouteKey, str],
    direct: dict[RouteKey, str],
) -> dict[RouteKey, tuple[str, str]]:
    """Classify every CUTOVER row: {(method,path): (class, source)}.

    Precedence is satellite wave, then direct dispatch, then the
    committed tier-C set, then the committed survivor set; anything
    left is a disclosed relay blocker.

    Args:
        cut: CUTOVER (method, path, family) triples.
        waves: Wave-table coverage from sat_wave_tables.
        direct: Direct-route coverage from sat_direct_routes.

    Returns:
        Mapping of route keys to (class, source-tag) pairs.

    Raises:
        SystemExit: On tier-C/satellite coverage overlap.
    """
    tierc = set()
    for r in TIER_C_ROUTES:
        tierc.add(tuple(r.split(" ", 1)))
    surv = set()
    for r in SURVIVOR_ROUTES:
        surv.add(tuple(r.split(" ", 1)))
    overlap = (set(waves) | set(direct)) & tierc
    if overlap:
        raise SystemExit(f"tier-C/satellite overlap: {sorted(overlap)}")
    result = {}
    for method, path, _family in cut:
        key = (method, path)
        if key in waves:
            result[key] = ("native-served", f"wave:{waves[key]}")
        elif key in direct:
            result[key] = ("native-served", f"direct:{direct[key]}")
        elif key in tierc:
            result[key] = ("tier-C-dispatch", "tracker:#10491-tier-C")
        elif key in surv:
            result[key] = ("relay-disclosed", "survivor:#8872")
        else:
            result[key] = ("relay-disclosed", "blocker:relay-leg")
    return result


def main(argv: list[str] | None = None) -> None:
    """Expose derivation for the bash checker without duplicating logic.

    Args:
        argv: CLI args (mode rows|coverage|pins|mounts|fragments).

    Raises:
        SystemExit: On missing required flags or derivation failures.
    """
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", required=True)
    ap.add_argument("--sat", default="")
    ap.add_argument("--sha", default=SATELLITE_SHA)
    ap.add_argument("--mode", required=True,
                    choices=["rows", "coverage", "pins", "mounts", "fragments"])
    args = ap.parse_args(argv)
    if args.mode == "rows":
        for method, path, fam in cutover_routes(args.root):
            print(f"{method} {path}\t{fam}")
    elif args.mode == "coverage":
        if not args.sat:
            raise SystemExit("--sat required for coverage")
        tables = sat_wave_tables(args.sat, args.sha)
        direct = sat_direct_routes(args.sat, args.sha)
        cut = cutover_routes(args.root)
        cls = classify(cut, tables, direct)
        for (method, path), (c, src) in sorted(cls.items()):
            print(f"{method} {path}\t{c}\t{src}")
    elif args.mode == "pins":
        w8 = w8_row_standing(args.root)
        for (method, path), (_fam, _c1, _c2, pin) in sorted(w8.items()):
            print(f"{method} {path}\t{pin or 'none'}")
    elif args.mode == "mounts":
        idx = mount_index(args.root)
        for (method, path), files in sorted(idx.items()):
            print(f"{method} {path}\t{','.join(files)}")
    elif args.mode == "fragments":
        idx = fragment_index(args.root)
        for path, files in sorted(idx.items()):
            print(f"{path}\t{','.join(files)}")
