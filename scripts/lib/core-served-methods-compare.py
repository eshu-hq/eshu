#!/usr/bin/env python3
"""Compare the satellite served-operation list against the core spec dump.

Reads the satellite openapi snapshot (argv 1) and the core spec row dump
(argv 2), fails on unreconciled drift, and prints the frozen row list to
stdout. Invoked by scripts/gen-core-served-methods.sh; the row counts are
pinned here because the frozen list they produce is pinned by
go/internal/query/core_served_methods_test.go.
"""

import json
import sys

spec = json.load(open(sys.argv[1]))
served = sorted(spec["served_operations"])
core = sorted(line.strip() for line in open(sys.argv[2]) if line.strip())
# Core-owned probe rows live only in the core spec by design (the satellite
# answers them dest-side or by delegation; they are not forwarded rows).
drift = sorted(set(served) ^ set(core) - {"GET /health", "GET /readyz"})
if drift:
    print(
        "satellite/core spec drift (must be reconciled before freezing):",
        file=sys.stderr,
    )
    for row in drift:
        print("  " + row, file=sys.stderr)
    sys.exit(1)
frozen = sorted(set(served) | {"GET /health", "GET /readyz"})
assert len(served) == 279, len(served)
assert len(frozen) == 281, len(frozen)
print(f"satellite served: {len(served)}, frozen: {len(frozen)}", file=sys.stderr)
print("\n".join(frozen))
