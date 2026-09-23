#!/usr/bin/env python3
# CI live-test targets for scripts/run-live-backend-tests.sh (#6784).
# Reads specs/live-tests.v1.yaml and prints one line per CI-class row:
#   <repo-root-relative file>|<module-relative package>|<Test|names>
# as a file (not a bash heredoc) so the heredoc-budget gate stays green.
#
# Usage: live_backend_test_targets.py <ledger_path> <repo_root>
import os
import re
import sys

if len(sys.argv) != 3:
    sys.exit(f"usage: {sys.argv[0]} <ledger_path> <repo_root>")
ledger_path, repo_root = sys.argv[1], sys.argv[2]
text = open(ledger_path).read()

rows = re.findall(
    r"^  - file: (\S+)\n    tag: .*\n    class: (\S+)\n    reason: ([^\n]*)(?:\n    backends: (\S+))?",
    text,
    re.M,
)
targets = []
for path, cls, backends in [(p, c, b or "both") for p, c, _, b in rows]:
    if cls != "ci":
        continue
    if backends not in ("nornicdb", "neo4j", "both"):
        sys.exit(f"CI-class row has invalid backends {backends!r}: {path}")
    # go test runs with go/ as its working directory (the module root),
    # so package paths are module-relative, not repo-root-relative.
    if not path.startswith("go/"):
        sys.exit(f"CI-class row outside go/: {path}")
    package = "./" + "/".join(path.split("/")[1:-1])
    with open(os.path.join(repo_root, path)) as handle:
        src = handle.read()
    tests = re.findall(r"^func (Test\w+)\(", src, re.M)
    if not tests:
        sys.exit(f"CI-class row defines no Test funcs: {path}")
    targets.append(f"{path}|{package}|{'|'.join(tests)}|{backends}")
if not targets:
    sys.exit("ledger names no CI-class rows")
sys.stdout.write("\n".join(sorted(targets)))
