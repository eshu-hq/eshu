#!/usr/bin/env bash
# Regenerates go/internal/query/testdata/core_served_methods_frozen.txt.
#
# Mechanical enumeration of the forwarded serving surface (Wave-6 D4.1):
# every Method+Path the satellite at the pinned commit answers (each is
# forwardable to core via a write-through lane, an explicit foreign-cred
# relay, or the generic /api/ catch-all), plus the two core-owned probes.
#
# Usage: bash scripts/gen-core-served-methods.sh [--sat-repo PATH] [--sat-pin REV]
#
# Requires: git, python3, and the repo-pinned Go toolchain on PATH.
set -euo pipefail

SAT_REPO="${SAT_REPO:-$HOME/Development/personal-repos/eshu-api}"
SAT_PIN="08ebde4"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --sat-repo) SAT_REPO="$2"; shift 2 ;;
    --sat-pin) SAT_PIN="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

ROOT="$(git rev-parse --show-toplevel)"
OUT="$ROOT/go/internal/query/testdata/core_served_methods_frozen.txt"
TMPDIR_WORK="$(mktemp -d)"

git -C "$SAT_REPO" show "$SAT_PIN:openapi/spec_snapshot.json" > "$TMPDIR_WORK/sat_spec.json"

# Dump the core OpenAPI served rows through a throwaway test so the
# enumeration reads the same assembled spec the binary serves.
DUMP_TEST="$ROOT/go/internal/query/zz_dump_spec_test.go"
cat > "$DUMP_TEST" <<'EOF'
package query

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

func TestZZDumpSpec(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for p, ops := range spec.Paths {
		for m := range ops {
			rows = append(rows, strings.ToUpper(m)+" "+p)
		}
	}
	sort.Strings(rows)
	if err := os.WriteFile(os.Getenv("ESHU_SPEC_DUMP_OUT"), []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
EOF
export ESHU_SPEC_DUMP_OUT="$TMPDIR_WORK/core_spec.txt"
trap 'rm -f "$DUMP_TEST"' EXIT
(cd "$ROOT/go" && go test ./internal/query/ -run TestZZDumpSpec -count=1 >/dev/null)

python3 - "$TMPDIR_WORK/sat_spec.json" "$TMPDIR_WORK/core_spec.txt" > "$OUT" <<'EOF'
import json, sys
spec = json.load(open(sys.argv[1]))
served = sorted(spec['served_operations'])
core = sorted(l.strip() for l in open(sys.argv[2]) if l.strip())
drift = sorted(set(served) ^ set(core) - {'GET /health'})
if drift:
    print('satellite/core spec drift (must be reconciled before freezing):', file=sys.stderr)
    for row in drift:
        print('  ' + row, file=sys.stderr)
    sys.exit(1)
frozen = sorted(set(served) | {'GET /health', 'GET /readyz'})
assert len(served) == 279, len(served)
assert len(frozen) == 281, len(frozen)
print(f'satellite served: {len(served)}, frozen: {len(frozen)}', file=sys.stderr)
print('\n'.join(frozen))
EOF

# Prepend the derivation header (kept here, not in testdata, so the frozen
# file itself stays a pure row list after the comment block).
HEADER="$(cat <<'HDR'
# Frozen core served-method list (Wave-6, 2026-11-19).
#
# Mechanical enumeration at the satellite pin 08ebde4 (v0.6.0) plus the two
# core-owned probe rows. Derivation:
#   1. openapi/spec_snapshot.json served_operations at 08ebde4 (279 rows):
#      every Method+Path the satellite answers. Each is forwardable to core:
#      write-through lanes forward unconditionally; terminated lanes relay
#      explicitly for foreign credential classes; anything else under /api/*
#      crosses the generic relay catch-all (api/router.go catch-all).
#   2. GET /health: served dest-side by the satellite probes, documented in
#      the core OpenAPI spec, mounted by query.APIRouter.
#   3. GET /readyz: new core-owned backend-gated probe (Wave-6 serve.go).
# Floor check: contains the frozen 7-rider base rows' spellings only as
# judged (riders are catch-all spellings, answered by core as 404/405) and
# all 19 survivor mutations. Regenerate with
# scripts/gen-core-served-methods.sh; the test
# go/internal/query/core_served_methods_test.go fails on any drift.
HDR
)"
{
  printf '%s\n' "$HEADER"
  cat "$OUT"
} > "$TMPDIR_WORK/frozen.txt"
mv "$TMPDIR_WORK/frozen.txt" "$OUT"
rm -rf "$TMPDIR_WORK"
echo "wrote $OUT"
