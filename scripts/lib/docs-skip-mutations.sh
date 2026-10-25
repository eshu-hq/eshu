#!/usr/bin/env bash
# Scratch-tree mutations for the docs-only skip guard proof.
# GNU sed -i breaks on macOS, and python3 is already a hard dependency of the
# harness, so every mutation below is a python3 in-place edit. Sourced, never
# executed directly: the functions operate on the harness scratch tree.

# Disable the planned-build caller: the guard must fail.
docs_skip_mutate_disable_planned_build() {
  python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
with open(path) as source:
    lines = source.read().splitlines(keepends=True)
with open(path, 'w') as target:
    for line in lines:
        if line.startswith('  call-build:'):
            line = line.replace(' && contains(', ' && false && contains(', 1)
        target.write(line)
PY
}

# Drop the trusted docs filter: the guard must fail.
docs_skip_mutate_drop_docs_filter() {
  python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
with open(path) as source:
    lines = source.read().splitlines(keepends=True)
with open(path, 'w') as target:
    for line in lines:
        if line.rstrip('\n') == '  - docs/**':
            continue
        target.write(line)
PY
}

# Drop the selected-full Go assertion: the guard must fail.
docs_skip_mutate_drop_lane_assertion() {
  python3 - "$1" <<'PY'
import sys
needle = 'selected full ${LANE} result='
path = sys.argv[1]
with open(path) as source:
    lines = source.read().splitlines(keepends=True)
with open(path, 'w') as target:
    for line in lines:
        if needle in line:
            continue
        target.write(line)
PY
}

# Drop the Go umbrella assertion call: the guard must fail.
docs_skip_mutate_drop_go_umbrella_call() {
  python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
with open(path) as source:
    content = source.read()
assert content.count('run: bash scripts/ci/assert-go-complete.sh') == 2
with open(path, 'w') as target:
    target.write(content.replace('run: bash scripts/ci/assert-go-complete.sh', 'run: true', 1))
PY
}

# Install an `awk` shim into <dir> that releases the first 1024 bytes of output
# at once and the rest 0.2s later. It makes a short-lived reader such as
# `rg -q` exit while a producer is still writing, which is the interleaving
# behind the intermittent pipefail/SIGPIPE false failure in job_needs. The
# real awk is resolved before <dir> is put on PATH.
docs_skip_install_slow_awk() {
  local dir="$1" real_awk
  real_awk="$(command -v awk)"
  mkdir -p "${dir}"
  printf '#!/usr/bin/env bash\n%s "$@" | { dd bs=1024 count=1 iflag=fullblock 2>/dev/null; sleep 0.2; cat; }\n' "${real_awk}" >"${dir}/awk"
  chmod +x "${dir}/awk"
}
