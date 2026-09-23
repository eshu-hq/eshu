#!/usr/bin/env python3
# Live-test ledger integrity check (#6784). Verifies that every
# *_live_test.go file under go/ is classified in specs/live-tests.v1.yaml
# with a valid class and a non-blank reason, and that every ledger row
# names a tracked or present file exactly once. An optional `backends:`
# line (nornicdb, neo4j, or both; default both) pins which backends the
# test can run on.
#
# Invoked by scripts/verify-live-tests-ledger.sh as a file (not a bash
# heredoc) so the heredoc-budget gate stays green: Homebrew bash >= 5.1
# deadlocks on heredoc bodies over 512 bytes.
#
# Usage: verify-live-tests-ledger.py <ledger_path> <repo_root>
import os
import re
import subprocess
import sys

if len(sys.argv) != 3:
    sys.exit(f"usage: {sys.argv[0]} <ledger_path> <repo_root>")
ledger_path, repo_root = sys.argv[1], sys.argv[2]
text = open(ledger_path).read()

rows = re.findall(
    r"^  - file: (\S+)\n    tag: (.*)\n    class: (\S+)\n    reason: ([^\n]*)(?:\n    backends: (\S+))?",
    text,
    re.M,
)
if not rows:
    sys.exit("no ledger rows parsed")

seen_files = set()
classes = {}
for path, tag, cls, reason, backends in rows:
    if path in seen_files:
        sys.exit(f"duplicate ledger row: {path}")
    seen_files.add(path)
    classes[path] = cls
    if cls not in ("ci", "scheduled", "retired"):
        sys.exit(f"invalid class {cls!r} for {path}")
    # Backend targeting defaults to both; a row naming a backend-specific
    # test pins the backends it can run on so the runner never schedules
    # a hardcoded-"nornic" test against Neo4j.
    if not backends:
        backends = "both"
    if backends not in ("nornicdb", "neo4j", "both"):
        sys.exit(f"invalid backends {backends!r} for {path}")
    if not reason.strip():
        sys.exit(f"blank reason for {path}")
    try:
        with open(os.path.join(repo_root, path)) as handle:
            src = handle.read()
    except OSError:
        src = ""
    build = re.search(r"^//go:build\s+(.+)$", src, re.M)
    actual = build.group(1).strip() if build else ""
    claimed = tag.strip().strip('"')
    if claimed == "~":
        claimed = ""
    if claimed != actual:
        sys.exit(f"tag mismatch for {path}: ledger {tag!r} vs file {actual!r}")

# --cached plus --others: a brand-new live test must be at least present on
# disk to be classified. Untracked-but-ignored build output stays excluded
# via --exclude-standard, so only real source surprises surface.
tracked = subprocess.run(
    ["git", "-C", repo_root, "ls-files", "--others", "--exclude-standard",
     "--cached", "go/", "*.go"],
    capture_output=True,
    text=True,
    check=True,
).stdout.split()
live = sorted(p for p in tracked if p.endswith("_live_test.go"))
if not live:
    sys.exit("no live test files found; is REPO_ROOT a repo checkout?")

# A live test under a nonstandard filename evades the *_live_test.go rule:
# any tracked *_test.go carrying a live_* build-tag atom and defining a
# Test func is a live backend proof too and must be classified. Helper-only
# files (live tag, no Test funcs) compile into the test binary and are
# covered transitively through the files that use them.
nonstandard = []
for path in tracked:
    if not path.endswith("_test.go") or path.endswith("_live_test.go"):
        continue
    try:
        with open(os.path.join(repo_root, path)) as handle:
            src = handle.read()
    except OSError:
        continue
    build = re.search(r"^//go:build\s+(.+)$", src, re.M)
    if not build:
        continue
    atoms = set(re.findall(r"[A-Za-z0-9_]+", build.group(1)))
    if not any(a.startswith("live_") for a in atoms):
        continue
    if re.search(r"^func Test\w+\(", src, re.M):
        nonstandard.append(path)
live = sorted(set(live) | set(nonstandard))

# Unclassified live tests surface before stale rows: in a sparse fixture
# tree the planted violation must read as unclassified, not as a row
# naming a file the fixture does not contain.
missing = [p for p in live if p not in seen_files]
if missing:
    sys.exit(f"{len(missing)} unclassified live test(s), first: {missing[:5]}")

for path in sorted(seen_files):
    full = os.path.join(repo_root, path)
    if not os.path.isfile(full):
        sys.exit(f"ledger row names missing file: {path}")

# A retired row names a file that was a live proof and was repaired in
# place, so it still exists but no longer meets the live definition. A
# row for a deleted file is removed in the deleting commit instead: the
# missing-file check above keeps typos fail-closed. Retiring a file that
# is still live hides a live proof, so it fails: repair it first.
still_live = [p for p in live if classes.get(p) == "retired"]
if still_live:
    sys.exit(f"{len(still_live)} retired row(s) name still-live file(s), first: {still_live[:5]}")

retired = {p for p, cls in classes.items() if cls == "retired"}
extra = sorted(seen_files - set(live) - retired)
if extra:
    sys.exit(f"{len(extra)} ledger row(s) name non-live or untracked files, first: {extra[:5]}")

print(f"live-tests ledger ok: {len(rows)} rows, {len(live)} live files classified")
