#!/usr/bin/env python3
# Queue-contract coverage ledger checker (#11354). Validates
# specs/queue-contract-coverage.v1.yaml: one row per queue/claim/lease
# invariant with its contract_ref, proving Test funcs, runner, class and
# build tag, plus a symbols: map from each covered SQL symbol to its row.
# Usage: verify-queue-contract-coverage.py <ledger_path> <repo_root>
import json
import os
import re
import subprocess
import sys
from pathlib import Path


CLASSES = ("live", "env_gated", "fake", "missing")
RUNNERS = (
    "live-postgres-readiness",
    "live-postgres-migrations",
    "postgres-scheduled",
    "reducer-contention",
    "manual-dsn",
    "unit",
    "none",
)
LIVE_RUNNERS = (
    "live-postgres-readiness",
    "live-postgres-migrations",
    "postgres-scheduled",
    "reducer-contention",
)
READINESS_MARKER = "//eshu:readiness live-postgres-readiness"


def fail(message):
    sys.exit(message)


def parse_ledger(text):
    """Split the ledger into row chunks and a symbol map (line-based)."""
    if not text.startswith("version: queue-contract-coverage/v1\n"):
        fail("ledger must start with version: queue-contract-coverage/v1")
    rows_at = text.find("\nrows:\n")
    symbols_at = text.find("\nsymbols:\n")
    if rows_at < 0 or symbols_at < 0 or symbols_at < rows_at:
        fail("ledger needs rows: and symbols: sections in order")
    rows_text = text[rows_at + len("\nrows:\n"):symbols_at]
    symbols_text = text[symbols_at + len("\nsymbols:\n"):]
    chunks = re.split(r"\n  - contract_ref: ", "\n" + rows_text)
    rows = []
    for chunk in chunks:
        chunk = chunk.strip("\n")
        if not chunk.strip():
            continue
        rows.append(parse_row(chunk))
    symbols = {}
    for line in symbols_text.splitlines():
        if not line.strip() or line.strip().startswith("#"):
            continue
        match = re.match(r"^  (\w+): (\S+)\s*$", line)
        if not match:
            fail(f"bad symbols line: {line!r}")
        name, ref = match.group(1), match.group(2)
        if name in symbols:
            fail(f"duplicate symbols entry: {name}")
        symbols[name] = ref
    return rows, symbols


def parse_row(chunk):
    """Parse one row chunk into fields and proving-test entries."""
    lines = chunk.splitlines()
    ref = lines[0].strip()
    if not ref:
        fail("empty contract_ref")
    fields = {}
    proving = []
    current = None
    for line in lines[1:]:
        if line.startswith("    proving_tests:"):
            current = "proving"
            continue
        if current == "proving":
            file_match = re.match(r"^      - file: (\S+)\s*$", line)
            tests_match = re.match(r"^        tests: \[(.*)\]\s*$", line)
            if file_match:
                proving.append({"file": file_match.group(1), "tests": []})
            elif tests_match:
                if not proving:
                    fail(f"tests without file in row {ref}")
                names = [n.strip() for n in tests_match.group(1).split(",")]
                proving[-1]["tests"] = [n for n in names if n]
            elif line.strip() and not line.startswith("        "):
                fail(f"bad proving_tests line in row {ref}: {line!r}")
            continue
        match = re.match(r"^    (\w+): (.*)$", line)
        if not match:
            fail(f"bad row line in {ref}: {line!r}")
        fields[match.group(1)] = match.group(2).strip()
    return {"contract_ref": ref, "fields": fields, "proving_tests": proving}


def read_build_tag(repo_root, path):
    try:
        with open(os.path.join(repo_root, path)) as handle:
            src = handle.read()
    except OSError:
        return None, ""
    match = re.search(r"^//go:build\s+(.+)$", src, re.M)
    actual = match.group(1).strip() if match else ""
    return src, actual


def symbol_in_code(repo_root, symbol):
    try:
        result = subprocess.run(
            ["rg", "-l", "--max-count=1", "-e", r"\b" + symbol + r"\b",
             "--glob", "*.go", "go"],
            cwd=repo_root, capture_output=True, text=True, check=False)
    except OSError as error:
        fail(f"symbol search failed: {error}")
    return bool(result.stdout.strip())


def live_enrolled(repo_root, runner, path):
    """Check a proving file is enrolled under a live runner."""
    full = os.path.join(repo_root, path)
    try:
        with open(full) as handle:
            src = handle.read()
    except OSError:
        return False
    if runner == "live-postgres-readiness":
        # The marker is the enrollment source: the readiness generator
        # derives its inventory, fragments and workflow env from these
        # markers, and its drift gate fails on any skew, so the marker
        # alone proves enrollment here.
        return READINESS_MARKER in src
    if runner == "postgres-scheduled":
        frag_dir = Path(repo_root) / "specs" / "live-tests.d"
        for frag in sorted(frag_dir.glob("*.yaml")):
            try:
                frag_text = frag.read_text(encoding="utf-8")
            except OSError:
                continue
            for block in re.finditer(
                    r"^  - file: (\S+)\n((?:    .*\n)+)", frag_text, re.M):
                if block.group(1) == path and \
                        "    runner: postgres-scheduled\n" in block.group(2):
                    return True
        return False
    if runner == "reducer-contention":
        inv = Path(repo_root) / "scripts" / "lib" / \
            "reducer_contention_inventory.json"
        try:
            data = json.loads(inv.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return False
        files = data.keys() if isinstance(data, dict) else data
        return path in files
    return False


def main():
    if len(sys.argv) != 3:
        fail(f"usage: {sys.argv[0]} <ledger_path> <repo_root>")
    ledger_path, repo_root = sys.argv[1], sys.argv[2]
    try:
        text = Path(ledger_path).read_text(encoding="utf-8")
    except OSError as error:
        fail(f"ledger not found: {ledger_path}: {error}")
    rows, symbols = parse_ledger(text)
    if not rows:
        fail("no ledger rows parsed")

    seen_refs = set()
    for row in rows:
        ref = row["contract_ref"]
        if ref in seen_refs:
            fail(f"duplicate ledger row: {ref}")
        seen_refs.add(ref)
        fields = row["fields"]
        cls = fields.get("class", "")
        if cls not in CLASSES:
            fail(f"invalid class {cls!r} for {ref}")
        runners = [r.strip() for r in
                   fields.get("runners", "").strip("[]").split(",")]
        runners = [r for r in runners if r]
        for runner in runners:
            if runner not in RUNNERS:
                fail(f"unknown runner {runner!r} for {ref}")
        if fields.get("blocking", "") != "true":
            fail(f"row {ref} must be blocking: true")
        if cls == "live" and not any(r in LIVE_RUNNERS for r in runners):
            fail(f"live row {ref} needs a live-Postgres runner")
        if cls == "live" and any(r not in LIVE_RUNNERS for r in runners):
            fail(f"live row {ref} cannot mix in a non-live runner")
        if cls == "env_gated" and runners != ["manual-dsn"]:
            fail(f"env_gated row {ref} must use runners: [manual-dsn]")
        if cls == "fake" and runners != ["unit"]:
            fail(f"fake row {ref} must use runners: [unit]")
        if cls == "missing":
            if runners != ["none"]:
                fail(f"missing row {ref} must use runners: [none]")
            if row["proving_tests"]:
                fail(f"missing row {ref} must name no proving tests")
        if cls != "missing" and not row["proving_tests"]:
            fail(f"row {ref} names no proving tests")
        # A claim:/lease: row outside class live is tracked burn-down, not
        # proof: the unconditional blocking:true gate above keeps it from
        # ever going silent, and only a live-Postgres runner can promote it.

        reason = fields.get("reason", "")
        if not reason.lstrip().startswith('"') and (
                reason.lstrip().startswith("#")
                or re.search(r"\s#", reason)):
            fail(f"unquoted '#' in reason for {ref}: quote the full reason")
        if not reason.strip(' "'):
            fail(f"blank reason for {ref}")

        tag = fields.get("tag", "~").strip().strip('"')
        claimed = "" if tag == "~" else tag
        for entry in row["proving_tests"]:
            path = entry["file"]
            full = os.path.join(repo_root, path)
            if not os.path.isfile(full):
                fail(f"ledger row {ref} names missing file: {path}")
            if not entry["tests"]:
                fail(f"ledger row {ref} names {path} with no tests")
            src, actual = read_build_tag(repo_root, path)
            if src is None:
                fail(f"ledger row {ref} names missing file: {path}")
            if claimed != actual:
                fail(f"tag mismatch for {path}: ledger {tag!r} "
                     f"vs file {actual!r}")
            for name in entry["tests"]:
                if not re.search(r"^func " + re.escape(name) + r"\(",
                                 src, re.M):
                    fail(f"ledger row {ref} names missing test "
                         f"{name} in {path}")
            if cls == "live":
                if not any(live_enrolled(repo_root, r, path)
                           for r in runners):
                    fail(f"live row {ref} proving file {path} is not "
                         f"enrolled under {runners}")
            elif cls == "env_gated":
                if "t.Skip(" not in src or "DSN" not in src:
                    fail(f"env_gated row {ref} proving file {path} has "
                         f"no DSN skip")
            elif cls == "fake":
                if "sql.Open(" in src or READINESS_MARKER in src:
                    fail(f"fake row {ref} proving file {path} touches "
                         f"live Postgres")

    for name, ref in symbols.items():
        if ref not in seen_refs:
            fail(f"symbols entry {name} names unknown row {ref}")
        if not symbol_in_code(repo_root, name):
            fail(f"listed symbol absent from code: {name}")
    covered = [ref for ref in seen_refs
               if ref in set(symbols.values())]
    uncovered = sorted(seen_refs - set(covered))
    if uncovered:
        fail(f"rows with no covered symbol: {uncovered}")

    total_tests = sum(len(e["tests"]) for r in rows
                      for e in r["proving_tests"])
    print(f"queue-contract-coverage ok: {len(rows)} rows, "
          f"{len(symbols)} symbols, {total_tests} proving tests")


if __name__ == "__main__":
    main()
