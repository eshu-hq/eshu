#!/usr/bin/env bash
# Hermetic contract tests for scripts/ci/warm-go-race-cache.sh and its
# workflow lockstep with .github/workflows/test.yml.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
warmer="${repo_root}/scripts/ci/warm-go-race-cache.sh"

pass=0
fail=0
ok() {
	pass=$((pass + 1))
	printf 'PASS: %s\n' "$1"
}
bad() {
	fail=$((fail + 1))
	printf 'FAIL: %s\n' "$1" >&2
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

fake_go_dir="${tmp}/repo/go"
mkdir -p "${fake_go_dir}"
bin_dir="${tmp}/bin"
mkdir -p "${bin_dir}"

cat >"${bin_dir}/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "list" && "${2:-}" == "-json" && "${3:-}" == "./..." ]]; then
	case "${FAKE_GO_LIST_MODE:-normal}" in
	normal)
		cat <<'JSON'
{"ImportPath":"github.com/eshu-hq/eshu/go/internal/a","Imports":["fmt","C","github.com/eshu-hq/eshu/go/internal/b","github.com/aws/aws-sdk-go-v2/service/s3"],"TestImports":["testing","github.com/eshu-hq/eshu/go/internal/testutil"],"XTestImports":["k8s.io/client-go/kubernetes","fmt"]}
{"ImportPath":"github.com/eshu-hq/eshu/go/internal/b","Imports":["context","github.com/eshu-hq/eshu/go"],"TestImports":["github.com/aws/aws-sdk-go-v2/service/ec2"]}
JSON
		exit 0
		;;
	empty)
		exit 0
		;;
	invalid)
		printf '{not valid json\n'
		exit 0
		;;
	internal-only)
		printf '{"ImportPath":"github.com/eshu-hq/eshu/go/internal/a","Imports":["C","github.com/eshu-hq/eshu/go/internal/b"]}\n'
		exit 0
		;;
	esac
fi
if [[ "${1:-}" == "build" ]]; then
	printf '%s\n' "$*" >>"${GO_LOG_FILE}"
	if [[ "${FAKE_GO_BUILD_FAIL:-0}" == "1" ]]; then
		exit 1
	fi
	exit 0
fi
printf 'unexpected fake go args: %s\n' "$*" >&2
exit 99
EOF
chmod +x "${bin_dir}/go"

# 1. Normal mode: extracts unique sorted external/stdlib imports across Imports,
#    TestImports, and XTestImports, excludes C and internal packages, then builds ./...
log_file="${tmp}/go-normal.log"
: >"${log_file}"
if PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${log_file}" \
	GO_RACE_CACHE_WARM_GO_DIR="${fake_go_dir}" \
	bash "${warmer}" >/dev/null; then
	line1="$(sed -n '1p' "${log_file}")"
	line2="$(sed -n '2p' "${log_file}")"
	want1="build -race context fmt github.com/aws/aws-sdk-go-v2/service/ec2 github.com/aws/aws-sdk-go-v2/service/s3 k8s.io/client-go/kubernetes testing"
	want2="build -race ./..."
	if [[ "${line1}" == "${want1}" && "${line2}" == "${want2}" && "$(wc -l <"${log_file}" | tr -d ' ')" -eq 2 ]]; then
		ok "warm-go-race-cache.sh builds deduplicated sorted external/stdlib imports then ./..."
	else
		bad "warm-go-race-cache.sh unexpected go invocations: line1=[${line1}] line2=[${line2}]"
	fi
else
	bad "warm-go-race-cache.sh failed on valid fake go list"
fi

# 2. Fail closed on empty go list output.
if PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${tmp}/go-empty.log" \
	FAKE_GO_LIST_MODE=empty GO_RACE_CACHE_WARM_GO_DIR="${fake_go_dir}" \
	bash "${warmer}" >/dev/null 2>&1; then
	bad "warm-go-race-cache.sh must fail on empty go list output"
else
	ok "warm-go-race-cache.sh fails closed on empty go list output"
fi

# 3. Fail closed on invalid JSON from go list.
if PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${tmp}/go-invalid.log" \
	FAKE_GO_LIST_MODE=invalid GO_RACE_CACHE_WARM_GO_DIR="${fake_go_dir}" \
	bash "${warmer}" >/dev/null 2>&1; then
	bad "warm-go-race-cache.sh must fail on invalid JSON from go list"
else
	ok "warm-go-race-cache.sh fails closed on invalid JSON from go list"
fi

# 4. Fail closed when no external/stdlib imports are discovered.
if PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${tmp}/go-internal.log" \
	FAKE_GO_LIST_MODE=internal-only GO_RACE_CACHE_WARM_GO_DIR="${fake_go_dir}" \
	bash "${warmer}" >/dev/null 2>&1; then
	bad "warm-go-race-cache.sh must fail when zero external/stdlib imports are found"
else
	ok "warm-go-race-cache.sh fails closed when zero external/stdlib imports are found"
fi

# 5. Fail closed when go build -race fails.
if PATH="${bin_dir}:${PATH}" GO_LOG_FILE="${tmp}/go-build-fail.log" \
	FAKE_GO_BUILD_FAIL=1 GO_RACE_CACHE_WARM_GO_DIR="${fake_go_dir}" \
	bash "${warmer}" >/dev/null 2>&1; then
	bad "warm-go-race-cache.sh must propagate go build -race failures"
else
	ok "warm-go-race-cache.sh propagates go build -race failures"
fi

# 6. Workflow lockstep: go-race-cache-warm.yml must match test.yml CGO_CFLAGS,
#    GOCACHE, and gobuildrace primary cache key so warmed CGo/Go artifacts hit.
check_parity() {
	python3 - "$1" "$2" <<'PY'
import re
import sys
from pathlib import Path

test_yml = Path(sys.argv[1]).read_text(encoding="utf-8")
warm_yml = Path(sys.argv[2]).read_text(encoding="utf-8")

def extract_single(text, pattern, label):
    matches = re.findall(pattern, text, re.M)
    if len(matches) != 1:
        raise SystemExit(f"{label}: expected 1 match, got {len(matches)}")
    return matches[0].strip()

test_cgo = extract_single(test_yml, r'^\s*CGO_CFLAGS:\s*(.+)$', "test.yml CGO_CFLAGS")
warm_cgo = extract_single(warm_yml, r'^\s*CGO_CFLAGS:\s*(.+)$', "go-race-cache-warm.yml CGO_CFLAGS")
if test_cgo != warm_cgo:
    raise SystemExit(f"CGO_CFLAGS drift: test.yml={test_cgo!r} warm={warm_cgo!r}")

test_key = extract_single(test_yml, r'^\s*key:\s*(\$\{\{\s*runner\.os\s*\}\}.*gobuildrace-.*)$', "test.yml gobuildrace key")
warm_key = extract_single(warm_yml, r'^\s*key:\s*(\$\{\{\s*runner\.os\s*\}\}.*gobuildrace-.*)$', "go-race-cache-warm.yml gobuildrace key")
if test_key != warm_key:
    raise SystemExit(f"gobuildrace key drift: test.yml={test_key!r} warm={warm_key!r}")

warm_gocache = extract_single(warm_yml, r'GOCACHE:\s*([^},\s]+)', "go-race-cache-warm.yml GOCACHE")
if warm_gocache != "/tmp/eshu-ci/go-build":
    raise SystemExit(f"unexpected GOCACHE in warmer: {warm_gocache!r}")
if "GOCACHE: /tmp/eshu-ci/go-build" not in test_yml:
    raise SystemExit("test.yml missing GOCACHE: /tmp/eshu-ci/go-build")
PY
}

test_wf="${repo_root}/.github/workflows/test.yml"
warm_wf="${repo_root}/.github/workflows/go-race-cache-warm.yml"
if check_parity "${test_wf}" "${warm_wf}"; then
	ok "go-race-cache-warm.yml matches test.yml CGO_CFLAGS, GOCACHE, and gobuildrace key"
else
	bad "go-race-cache-warm.yml drifted from test.yml"
fi

# 7. Seeded-violation RED proof for workflow parity check (CGO_CFLAGS + key drift).
mut_cgo="${tmp}/warm-bad-cgo.yml"
sed 's/-std=gnu17/-std=gnu11/' "${warm_wf}" >"${mut_cgo}"
if check_parity "${test_wf}" "${mut_cgo}" >/dev/null 2>&1; then
	bad "parity check must fail when CGO_CFLAGS drifts"
else
	ok "seeded RED: parity check rejects drifted CGO_CFLAGS"
fi

mut_key="${tmp}/warm-bad-key.yml"
sed 's/gobuildrace-v2-/gobuildrace-v99-/' "${warm_wf}" >"${mut_key}"
if check_parity "${test_wf}" "${mut_key}" >/dev/null 2>&1; then
	bad "parity check must fail when gobuildrace key drifts"
else
	ok "seeded RED: parity check rejects drifted gobuildrace cache key"
fi

printf 'Summary: %d passed, %d failed\n' "${pass}" "${fail}"
[[ "${fail}" -eq 0 ]]
