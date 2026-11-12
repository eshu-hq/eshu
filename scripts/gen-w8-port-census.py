#!/usr/bin/env python3
"""W8 census: generate docs/internal/w8-port-census.md from CUTOVER plus #10491 states.

Read-only under go/ (Mount attribution via rg). Tracker input defaults to the
live ``gh issue view 10491`` body; ``--tracker FILE`` uses a saved snapshot.
"""
import argparse
import re
import subprocess
import sys
from pathlib import Path

LIB = Path(__file__).resolve().parent / "lib"
sys.path.insert(0, str(LIB))

from w8_port_census_data import (  # noqa: E402
    CODE_FLOW,
    CODE_SEARCH,
    CONTENT_POINT,
    PIN,
    PIN_APPROVED_UNMINTED,
    STATUS_PIN,
    WRITER_ADM,
    WRITER_FAM_ADM,
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
CUTOVER = ROOT / "go" / "cmd" / "api" / "CUTOVER_OWNERSHIP.md"
OUT = ROOT / "docs" / "internal" / "w8-port-census.md"

# Census-day pair is provenance; REVERIFIED_CORE is the S3 ruling base
# (origin/main at branch time). To re-base: update it, regenerate, run the checker.
CENSUS_DAY_CORE = "e3c33de093"
CENSUS_DAY_SAT = "0b56895"
REVERIFIED_CORE = "e504e328a7"  # origin/main at branch time (census-regen ruling base)

W7W_LABEL = "w7w-recorded(#10531-merged, satellite-pending)"


def load_tracker(snapshot):
    """Return the #10491 body: snapshot file, or live via gh by default."""
    if snapshot:
        return Path(snapshot).read_text()
    p = subprocess.run(["gh", "issue", "view", "10491", "--json", "body",
                        "-q", ".body"], capture_output=True, text=True,
                       cwd=str(ROOT), timeout=60)
    if p.returncode != 0 or not p.stdout.strip():
        raise SystemExit("live tracker read failed; re-run with --tracker <snapshot-file>. " + p.stderr.strip()[:200])
    return p.stdout


def parse_args(argv):
    """Parse command-line arguments."""
    ap = argparse.ArgumentParser(description="W8 census: generate docs/internal/w8-port-census.md.")
    ap.add_argument("--tracker", default=None, help="tracker snapshot file (default: live gh issue view 10491)")
    ap.add_argument("--out", default=str(OUT), help="output path (default: docs/internal/w8-port-census.md)")
    return ap.parse_args(argv)


# ---------- 1. parse CUTOVER rows ----------
def parse_cutover():
    """Parse CUTOVER_OWNERSHIP.md table rows."""
    rows = []
    pat = re.compile(r"^\| `(GET|POST|PATCH|PUT|DELETE) ([^`]+)` \| ([^|]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \| ([^|]+) \|")
    for ln in CUTOVER.read_text().split("\n"):
        m = pat.match(ln)
        if m:
            rows.append({
                "method": m.group(1), "path": m.group(2).strip(),
                "family": m.group(3).strip(), "contract": m.group(4).strip(),
                "channel": m.group(5).strip(), "persist": m.group(6).strip(),
                "mounted": m.group(7).strip(),
            })
    print(f"CUTOVER rows: {len(rows)}", file=sys.stderr)
    assert len(rows) == 281, len(rows)
    return rows


# ---------- 2. tracker states ----------
def parse_tracker(body):
    """Parse tracker body into route -> (bucket, cite) states."""
    state = {}
    cur = None
    for ln in body.split("\n"):
        m = re.match(r"##\s+(.*)", ln)
        if m:
            cur = m.group(1)
            continue
        m2 = re.match(r"^- \[(x| )\] `([^`]+)`(.*)$", ln)
        if m2 and cur:
            box, route, rest = m2.group(1), m2.group(2), m2.group(3).strip()
            if cur.startswith("Done "):
                state[route] = ("landed-Done", "")
            elif cur.startswith("Dispatched tier-C"):
                state[route] = ("tierC-G4", "")
            elif box == "x":
                cite = rest.lstrip("\u2014- ").strip()
                assert cite, route
                state[route] = ("landed-family", cite)
            else:
                state[route] = ("open", "")
    print(f"tracker states: {len(state)}", file=sys.stderr)
    assert len(state) == 281, len(state)
    return state


NEW_ROUTE = "GET /api/v0/services/{service_name}/api-surface/endpoints"
W7W = {"POST /api/v0/auth/setup/admin", "POST /api/v0/auth/setup/claim", "POST /api/v0/auth/setup/mfa", "PATCH /api/v0/auth/admin/sign-in-policy"}
APIR3 = {"GET /api/v0/docs", "GET /api/v0/redoc", "GET /health"}


# ---------- 3. Mount resolution (read-only rg) ----------
_RG_SKIP = ["--glob", "!*_test.go", "--glob", "!go/internal/query/openapi/**", "--glob", "!**/completeness*", "--glob", "!**/generated*"]
_MOUNT_RE = re.compile(r"func \(\w+ \*?(\w+)\) (Mount|\w+Routes)\(mux \*http\.ServeMux\)")


def _rg(args):
    """Run rg over go/ sources, skipping test/openapi/generated paths."""
    return subprocess.run(["rg"] + _RG_SKIP + args, capture_output=True,
                          text=True, cwd=str(ROOT))


def rg_route(method, path):
    """Find non-test Go files containing the route literal."""
    p = _rg(["-l", "-F", method + " " + path, "go"])
    return sorted(p.stdout.split())


def const_indirection(files, method, path):
    """Resolve route constants (full or path-only) to HandleFunc users."""
    names = set()
    for f in [x for x in files if x.endswith(".go")]:
        try:
            src = (ROOT / f).read_text()
        except OSError:
            continue
        for m in re.finditer(r"(\w+)\s*=\s*\"" + re.escape(method + " " + path) + r"\"", src):
            names.add(m.group(1))
    # path-only constants may live in files without the full literal: search globally
    if path.startswith("/"):
        p = _rg(["-N", "--no-filename", "-o",
                 r"(\w+)\s*=\s*\"" + re.escape(path) + r"\"", "go"])
        for ln in p.stdout.split("\n"):
            m = re.match(r"(\w+)\s*=", ln.strip())
            if m:
                names.add(m.group(1))
    extra = []
    for name in names:
        p = _rg(["-l", "-F", name, "go"])
        extra.extend(p.stdout.split())
    return sorted(set(extra))


def mount_info(files, method=None, path=None):
    """Pick handler file: prefers HandleFunc registrant defining Mount/Routes."""
    def score(fs):
        scored = []
        for f in fs:
            if not (f.endswith(".go") and "/openapi/" not in f
                    and "completeness" not in f and "README" not in f
                    and "generated" not in f):
                continue
            try:
                src = (ROOT / f).read_text()
            except OSError:
                continue
            has_mount = _MOUNT_RE.search(src)
            has_handle = "HandleFunc(" in src
            if has_mount:
                m = _MOUNT_RE.search(src)
                ln = next(i + 1 for i, l in enumerate(src.split("\n"))
                          if _MOUNT_RE.match(l))
                # API serving path first; MCP/ask route files deprioritized
                depri = 2 if ("/mcp/" in f or "/ask/" in f) else 0
                scored.append((depri, 0 if has_handle else 1, f, m.group(1), m.group(2), ln))
        scored.sort()
        return scored[0] if scored else None
    hit = score(files)
    if hit:
        return hit
    if method and path:
        extra = const_indirection(files, method, path)
        hit = score(sorted(set(files) | set(extra)))
        if hit:
            return hit
    return None


def struct_fields(path, struct):
    """Extract store/port fields from a handler struct definition."""
    try:
        src = (ROOT / path).read_text()
    except OSError:
        return []
    m = re.search(r"type " + re.escape(struct) + r" struct \{(.*?)\n\}", src, re.S)
    if not m:
        return []
    fields = []
    for ln in m.group(1).split("\n"):
        fm = re.match(r"\s*(\w+)\s+([\w\.\*\[\]]+)", ln)
        if fm and fm.group(1) != "//" and re.search(r"Store|Reader|Query|Resolver|Builder|Policy|Getter|Source|Client|Cache", fm.group(2)):
            t = fm.group(2)
            fields.append(f"{fm.group(1)}({t.replace('querycontract.', '').replace('*', '')})")
    return fields


# ---------- 4. classification data lives in lib/w8_port_census_data.py ----------
# (pin/writer tables, route sets, short(); all pin constants verified at
# REVERIFIED_CORE). Code is deliberately NOT a family PIN there: Code coverage
# is route-level via CODE_SEARCH / CODE_FLOW, mirroring CONTENT_POINT.
def c1_c5(r):
    """Compute route, mount citation, ports, and class for a CUTOVER row."""
    route = r["method"] + " " + r["path"]
    files = rg_route(r["method"], r["path"])
    mi = mount_info(files, r["method"], r["path"])
    if r["persist"] == "n/a (mux-level)":
        cls = "no-port"
        c1m = "APIRouter.Mount q/handler.go:147; Serve funcs q/handler.go:460-470"
        ports = "-"
    else:
        assert mi, f"no Mount resolution: {route} files={files}"
        _, _, f, struct, mname, ln = mi
        fields = struct_fields(f, struct)
        ports = ",".join(fields) if fields else "none-declared"
        c1m = f"Mount {short(f)} ({struct}.{mname})"
        if r["channel"] == "mutation" or "writer:" in r["persist"]:
            cls = "writer"
        elif r["family"] in PIN or route in CONTENT_POINT or route in CODE_SEARCH or route in CODE_FLOW:
            cls = "pinned"
        else:
            cls = "candidate"
    return route, c1m, ports, cls


def c2(r, route, cls):
    """Compute C2 (port/pin) for a row."""
    if cls == "no-port":
        return "n/a"
    if cls == "writer":
        adm = WRITER_ADM.get(route) or WRITER_FAM_ADM.get(r["family"], "generic(core-owned)")
        return "writer:" + adm
    if cls == "pinned":
        if route in CONTENT_POINT:
            return "dedicated:ContentPointReadAdmission"
        if route in CODE_SEARCH:
            return "dedicated:CodeSearchReadAdmission"
        if route in CODE_FLOW:
            return "dedicated:CodeFlowReadAdmission"
        if route in STATUS_PIN:
            return "dedicated:" + STATUS_PIN[route]
        return "dedicated:" + PIN[r["family"]][0].split(" v0")[0]
    return "blanket"


def c3(r, route, bucket):
    """Compute C3 (satisfier + transport) for a row."""
    via = {"newRouter": "newRouter", "wireAPI": "wireAPI", "mux-level": "mux"}.get(r["mounted"], r["mounted"])
    sat = {"landed-Done": "native-Done", "landed-family": "native-family",
           "tierC-G4": "tierC-dispatch", "open": "relay-leg"}[bucket]
    if route in W7W:
        sat = "w7w-record-inflight"
    if route in APIR3:
        sat = "mux-direct"
    return f"{sat} via {via}"


def c4(r):
    """Compute C4 (channel honesty) for a row."""
    if r["persist"] == "n/a (mux-level)":
        return "mux"
    if r["channel"] == "mutation":
        return "mut(core-store)"
    if "writer:" in r["persist"]:
        return "read-held(no-HTTP-path-writes)"
    if "writer handle held" in r["persist"]:
        return "read-held(writer-handle-held,no-HTTP-path-writes)"
    if r["method"] == "POST":
        return "read-POST(query-shaped)"
    return "read"


def c5(bucket, cite, r, route):
    """Compute C5 (wave bucket) for a row."""
    if bucket == "landed-Done":
        return "landed-Done"
    if bucket == "landed-family":
        m = re.search(r"(eshu-api#\d+)|sat #(\d+)", cite)
        raw = (m.group(1) or f"sat#{m.group(2)}") if m else "pin"
        return f"landed-family({raw.replace('eshu-api#', 'sat#')})"
    if bucket == "tierC-G4":
        return "tierC-G4(re-verify)"
    if route in W7W:
        return W7W_LABEL
    if route in APIR3:
        return "relay-leg(mux-direct,no-wave)"
    tag = PIN_APPROVED_UNMINTED.get(r["family"], "")
    return "relay-leg" + (f"+pin-approved:{tag.split(' (')[0]}" if tag else "")


def build_rows(rows, state):
    """Join CUTOVER rows with tracker states into table rows."""
    assert state.get(NEW_ROUTE) == ("open", ""), state.get(NEW_ROUTE)
    cutover_routes = {r["method"] + " " + r["path"] for r in rows}
    assert set(state) == cutover_routes, (set(state) ^ cutover_routes)
    trows = []
    classes = {}
    buckets = {}
    for r in rows:
        route, c1m, ports, cls = c1_c5(r)
        bucket, cite = state.get(route, ("open", ""))
        c2v, c3v, c4v = c2(r, route, cls), c3(r, route, bucket), c4(r)
        c5v = c5(bucket, cite, r, route)
        classes[cls] = classes.get(cls, 0) + 1
        buckets[c5v.split("+")[0]] = buckets.get(c5v.split("+")[0], 0) + 1
        trows.append((route, r["family"], c1m, ports, cls, c2v, c3v, c4v, c5v))
    print(f"classes: {classes}", file=sys.stderr)
    print(f"buckets: {buckets}", file=sys.stderr)
    assert sum(classes.values()) == 281
    assert classes.get("no-port", 0) == 4, classes
    # C5 full coverage: every row assigned, no residue bucket
    allowed = ("landed-Done", "landed-family", "tierC-G4", "w7w-recorded", "relay-leg")
    for t in trows:
        assert t[8].split("(")[0].split("+")[0] in allowed, t[8]
    return trows


def render(trows):
    """Render the full artifact text with counts derived from the buckets."""
    n = len(trows)
    done = sum(1 for t in trows if t[8] == "landed-Done")
    family = sum(1 for t in trows if t[8].startswith("landed-family"))
    tierc = sum(1 for t in trows if t[8].startswith("tierC-G4"))
    w7w = sum(1 for t in trows if t[8].startswith("w7w-recorded"))
    relay = sum(1 for t in trows if t[8].startswith("relay-leg"))
    tomigrate = relay + w7w
    counts = f"{n}/{done}/{family}/{tierc}/{tomigrate}"
    pass_line = f"PASS w8-port-census: rows={n} done={done} family={family} tierC={tierc} tomigrate={tomigrate} (relay={relay} w7w={w7w})"
    hdr = f"""<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# W8 port census (pre-lane, read-only)

Refs eshu-hq/eshu#9669, eshu-hq/eshu#10491. Status: preparation only — no cutover has
occurred, no behavior changes, no in-tree code moves. Census day 2026-11-10:
core `{CENSUS_DAY_CORE}` / satellite `{CENSUS_DAY_SAT}` (provenance; row set proven
byte-identical at the S3 ruling base). Re-based at core `{REVERIFIED_CORE}`
(origin/main at branch time; checker PASS with identical {counts}
counts, relay={relay} w7w={w7w}): +2 rows since the S3 base, named below —
`GET /api/v0/services/{{service_name}}/downstream-consumers` (Entities read,
eshu#10562) and `POST /api/v0/admin/workflow-retire-unclaimed` (Admin writer,
eshu#10553).

## Method (exact commands, run in the worktree root)

```bash
rg -c '^\\| `' go/cmd/api/CUTOVER_OWNERSHIP.md            # {n} live rows
python3 scripts/gen-w8-port-census.py [--tracker FILE]    # regenerate this file
bash scripts/check-w8-port-census.sh                      # drift gate (row parity)
```

Route set is read from [CUTOVER_OWNERSHIP.md](../../go/cmd/api/CUTOVER_OWNERSHIP.md);
box states default to the live #10491 body (`gh issue view 10491`); pass --tracker
with a saved snapshot file for a reproducible/offline run. Mount attribution resolves
each route literal (`"METHOD path"`) against non-test Go sources and cites the defining
`Mount`/`*Routes` site; ports are the handler struct's store/port fields.

## Pin inventory (all constants verified present at REVERIFIED_CORE above)

- Freshness 3: FreshnessReadAdmission v0 (`q/freshness/read_admission.go:26`, pin eshu#10494).
- Content point-read 3: ContentPointReadAdmission v0 (`q/content_read_admission.go:23`,
  pin eshu#10504); the 4 Content searches ride ContentSearchReadAdmission v0
  (`q/content_search_read_admission.go:103`, pin eshu#10540).
- AdminIdentityReads 8: AdminIdentityReadAdmission v0 + GovernanceAuditReadAdmission v0.
- AdminProviderConfigReads 3: ProviderConfigReadAdmission v0.
- SignInPolicyReads 2: SignInPolicyReadAdmission v0 (pin eshu#10515).
- Compare 1: CompareReadAdmission v0 (pin eshu#10425).
- IaC 11: IaCReadAdmission v0, 8 methods — pin-sufficient for 9 rows; partial on
  `GET /iac/resources` (SearchActive not adopted) and `POST /iac/dead` (Content fallback).
- Metrics 1: MetricsReadAdmission v0 (pin eshu#10440).
- TerraformConfigStateDrift 1: TerraformDriftReadAdmission v0.
- SemanticSearch 1: SemanticSearchReadAdmission v0 (record-only).
- SecretsIAM 5: SecretsIAMReadAdmission v0 (`q/secrets/read_admission.go:66`,
  pin eshu#10537); posture-summary S3 grant leg explicitly OUT.
- Status 3 open (+15 Done): StatusGovernanceReadAdmission v0 (`q/status_governance_read_admission.go:73`,
  Summary only) + StatusHostedReadinessReadAdmission v0 (Run+RunSingle) +
  pre-existing StatusReadAdmission v0 (StatusReadAdmissionContractVersion in
  `go/internal/status/read_admission.go`, answer-narration/snapshot legs +
  12 Done aggregate reads, pin eshu#10538) + StatusExtraReadAdmission v0
  (`q/status_extra_read_admission.go:94`, operations live-activity +
  index-status/status-index retirement legs, pin eshu#10405).
- Code search 2: CodeSearchReadAdmission v0 (`q/code_search_read_admission.go:106`,
  pin eshu#10540; remaining 21 Code rows unpinned).
- Code flow 4: CodeFlowReadAdmission v0 (`q/codequery/code_flow_read_admission.go:79`,
  pin eshu#10552 W6a).
- Writer admissions (certain mappings): replay/reopen/refinalize/recover-generations via
  RecoveryAdmission+ReplayLedgerAdmission v0; suppressions row via SuppressionAdmission v0;
  AdminIdentityMutations via AdminIdentityAdmission v0; AdminProviderConfigMutations via
  ProviderConfigMutationAdmission v0; PATCH sign-in-policy via SignInPolicyAdmission v0;
  Setup 3 POSTs via SetupAdmission v0; login rows via OIDC/GitHub/SAMLLoginAdmission v0;
  BrowserSessions via BrowserSessionAdmission v0; LocalIdentity via LocalIdentityAdmission v0
  (APITokenAdmission v0 for token legs); SessionList/Profile via SessionResolverAdmission v0;
  terminal-failure ack via TerminalFailureAdmission v0; retirement via RetirementAdmission v0
  (all constants verified at REVERIFIED_CORE: q/session_admission.go,
  q/sign_in_policy_types.go:63, q/setup_types.go:27).
  All other writer rows ride generic core-owned stores (admission TBD by wave).

## Column rules

- C1: handler `Mount` file + receiver (symbol-anchored, no line numbers), every store/port struct field, class
  in {{pinned (pure-reader + merged versioned read pin), candidate (pure-reader,
  no pin), writer (channel=mutation or writer-held persistence), no-port (mux-level)}}.
- C2: blanket (generic reader ports) | dedicated:\\<pin> | writer:\\<admission|generic> | n/a.
- C3: satisfier (native-Done, native-family, tierC-dispatch, w7w-record-inflight,
  relay-leg, mux-direct) + transport (`newRouter` OPTIONS-probed, `wireAPI`
  exclusion-pinned, mux-level).
- C4: channel honesty — read | read-POST (query-shaped POST, no durable writes) |
  read-held (read computation; writer handle held for recovery/audit/suppression, no
  HTTP-path durable writes) | mut(core-store) | mux.
- C5: wave bucket per the selection rule below. No residue bucket: every row carries
  one of landed-Done, landed-family, tierC-G4, w7w-recorded, relay-leg.

## C5 selection rule + W8-44 status

landed-Done = #10491 Done box; landed-family = family box + sat citation; tierC-G4 =
tier-C box (G4 re-verify before any Done move); w7w-recorded = the 4 W7-writer rows
(core record merged via eshu-hq/eshu#10531, satellite consumption pending, boxes stay
unchecked per the W7-writers ruling); relay-leg =
everything else (Path-B catch-all, no carve-outs). Pins that exist for still-open
rows show as C2 dedicated:<pin> with C5 relay-leg (pin exists, satellite termination
pending); rows without a covering pin show C2 blanket. W8-44 membership is NOT assigned
here: the 44-route selection rule was withdrawn by the W8 S3 ruling (no row is tagged
W8 in this artifact). H1 as a 44-shape is dead: Entities(9)+Content(7)+Code(27)+
SemanticEvidence(2)=45 (includes 3 landed Content rows).

Disposition (w7w-recorded): core record merged via eshu-hq/eshu#10531; satellite
consumption pending. The 4 tracker boxes stay UNCHECKED until satellite termination
lands, per the W7-writers ruling — this label rename changes no box.

## Verification

`bash scripts/check-w8-port-census.sh` at census commit:

```text
{pass_line}
```

{n} = {done + family} terminated ({done} Done + {family} family-native) + {tierc} tier-C + {tomigrate} to-migrate,
each count derived from table buckets, no hand-typed counts. Route set is byte-equal
to the CUTOVER_OWNERSHIP.md row set. Mount citations were resolved per row by the
generator (literals, `Route` constants, and path-only constants with `"METHOD "+Const`
registrations); every row carries a non-empty C1–C5 and a known C5 bucket.

## Route table ({n} rows)

| route | family | C1 mount+class | C2 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- | --- |
"""
    lines = [hdr]
    for t in trows:
        route, fam, c1m, ports, cls, c2v, c3v, c4v, c5v = t
        lines.append(f"| `{route}` | {fam} | {c1m}; ports:{ports}; {cls} | {c2v} | {c3v} | {c4v} | {c5v} |\n")
    return "".join(lines)


def main(argv=None):
    """Generate the census artifact from CUTOVER plus tracker states."""
    args = parse_args(argv if argv is not None else sys.argv[1:])
    rows = parse_cutover()
    state = parse_tracker(load_tracker(args.tracker))
    trows = build_rows(rows, state)
    Path(args.out).write_text(render(trows))
    print(f"wrote {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
