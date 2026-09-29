#!/usr/bin/env bash
# Static test for scripts/run-live-backend-tests.sh (#6784 slice B).
# Exercises argument parsing (both --flag value and --flag=value forms),
# the pinned-image drift check against the compose files, the ledger
# target extraction, and the failure modes that must stay loud — all
# without a Docker daemon, via ESHU_LIVE_RUNNER_SELFTEST=1 probes and
# stub binaries on PATH (no containers, no network).
# Fast, credential-free, Docker-free, network-free.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/run-live-backend-tests.sh"
targets="${repo_root}/scripts/lib/live_backend_test_targets.py"
ledger="${repo_root}/specs/live-tests.v1.yaml"

fail() {
	printf 'test-run-live-backend-tests: %s\n' "$*" >&2
	exit 1
}

[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] ||
	fail "test script must stay under 500 lines"
[[ -x "${script}" ]] || fail "runner missing or not executable"
[[ -f "${targets}" ]] || fail "target extractor missing"
[[ -f "${ledger}" ]] || fail "ledger missing"
bash -n "${script}" || fail "runner fails bash -n"

probe() {
	ESHU_LIVE_RUNNER_SELFTEST=1 bash "${script}" "$@" 2>&1
}

# ── GREEN: defaults parse ────────────────────────────────────────────────
out="$(probe)" || fail "default invocation failed"
[[ "${out}" == *"backend=both tags=live_nornicdb_answer_truth keep=0 use_compose=1"* ]] ||
	fail "unexpected defaults: ${out}"

# ── GREEN: space form parses identically to equals form ──────────────────
out="$(probe --backend nornicdb --tags foo --keep --no-compose)" || fail "space-form args failed"
[[ "${out}" == *"backend=nornicdb tags=foo keep=1 use_compose=0"* ]] ||
	fail "unexpected space-form parse: ${out}"
out="$(probe --backend=nornicdb --tags=foo)" || fail "equals-form args failed"
[[ "${out}" == *"backend=nornicdb tags=foo keep=0 use_compose=1"* ]] ||
	fail "unexpected equals-form parse: ${out}"

# ── RED: unknown flag and bad backend stay loud ──────────────────────────
probe --bogus >/dev/null 2>&1 && fail "unknown argument passed"
probe --backend oracle >/dev/null 2>&1 && fail "bad backend passed"
probe --backend >/dev/null 2>&1 && fail "missing --backend value passed"

# ── RED: unexpected CI tag fails extraction, not silent green ─────────────
# (a ci row with a different build tag would make go test -run match zero
# tests and exit 0; the extractor rejects it so promotion stays a ledger edit)
seed_dir="$(mktemp -d)"
trap 'rm -rf "${seed_dir}"' EXIT
printf 'version: 1\nrows:\n  - file: go/cmd/golden-corpus-gate/graph_row_tokens_live_test.go\n    tag: bogus_future_tag\n    class: ci\n    reason: seeded RED for the tag guard\n' >"${seed_dir}/ledger.yaml"
tag_err="$(python3 "${targets}" "${seed_dir}/ledger.yaml" "${repo_root}" 2>&1)" && fail "wrong-tag ci row accepted"
[[ "${tag_err}" == *"unexpected tag"* ]] || fail "wrong-tag rejection names no tag: ${tag_err}"

# ── RED: extractor crash dies naming extraction, not "no targets" ─────────
# (mapfile succeeds even when the process substitution fails, so the runner
# must capture the extractor failure explicitly)
mkdir -p "${seed_dir}/fakebin"
printf '#!/usr/bin/env bash\nexit 1\n' >"${seed_dir}/fakebin/python3"
chmod +x "${seed_dir}/fakebin/python3"
extract_err="$(PATH="${seed_dir}/fakebin:${PATH}" ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend both 2>&1)" &&
	fail "extractor crash produced a plan"
[[ "${extract_err}" == *"could not extract live-test targets"* ]] ||
	fail "extractor crash misreported: ${extract_err}"

# ── RED: failed up tears down its half-started stack ──────────────────────
# (the EXIT trap only knows stacks recorded after a successful start, so
# the up-failure branch must compose_down before dying; stubbed docker:
# `up` leaves a marker then fails, `down` clears it — no daemon involved)
cat >"${seed_dir}/fakebin/docker" <<'EOF'
#!/usr/bin/env bash
for a in "$@"; do
	if [[ "${a}" == "up" ]]; then touch "${ESHU_LIVE_RUNNER_STUB_MARKER}"; exit 1; fi
	if [[ "${a}" == "down" ]]; then rm -f "${ESHU_LIVE_RUNNER_STUB_MARKER}"; exit 0; fi
done
exit 0
EOF
chmod +x "${seed_dir}/fakebin/docker"
# The extraction-crash seed above left a failing python3 stub behind;
# this seed needs real extraction, so drop it (docker stays stubbed).
rm -f "${seed_dir}/fakebin/python3"
export ESHU_LIVE_RUNNER_STUB_MARKER="${seed_dir}/partial-stack"
up_err="$(PATH="${seed_dir}/fakebin:${PATH}" bash "${script}" --backend nornicdb 2>&1)" &&
	fail "failed up did not die"
[[ "${up_err}" == *"could not start nornicdb stack"* ]] ||
	fail "failed up misreported: ${up_err}"
[[ ! -f "${ESHU_LIVE_RUNNER_STUB_MARKER}" ]] ||
	fail "failed up littered its half-started stack"

# ── Neo4j runs on the Docker host's native platform (#7353) ──────────────
# (the pinned image is a multi-arch index; forcing linux/amd64 on an arm64
# host ran the JVM under emulation and made cold planning blow the 10s
# entity-context budget. An explicit NEO4J_PLATFORM still wins, and an
# unknown or unreadable arch leaves the compose file's own default.)
mkdir -p "${seed_dir}/archbin"
cat >"${seed_dir}/archbin/docker" <<'EOF'
#!/usr/bin/env bash
[[ "$1" == "version" && -n "${ESHU_STUB_DOCKER_ARCH:-}" ]] || exit 1
printf '%s\n' "${ESHU_STUB_DOCKER_ARCH}"
EOF
chmod +x "${seed_dir}/archbin/docker"
# platform_probe <docker arch> [NEO4J_PLATFORM]: the platform the runner
# resolves for neo4j with a stubbed docker reporting that server arch.
platform_probe() {
	local -a override=(-u NEO4J_PLATFORM)
	[[ -z "${2:-}" ]] || override=("NEO4J_PLATFORM=$2")
	env "${override[@]}" PATH="${seed_dir}/archbin:${PATH}" ESHU_STUB_DOCKER_ARCH="$1" \
		ESHU_LIVE_RUNNER_SELFTEST=1 bash "${script}" --backend neo4j 2>&1 |
		{ rg '^neo4j_platform=' || true; } | cut -d= -f2-
}
for case in arm64=linux/arm64 aarch64=linux/arm64 amd64=linux/amd64 x86_64=linux/amd64 s390x= =; do
	arch="${case%%=*}"
	want="${case#*=}"
	got="$(platform_probe "${arch}")"
	[[ "${got}" == "${want}" ]] ||
		fail "docker arch '${arch}' resolved neo4j platform '${got}', want '${want}'"
done
got="$(platform_probe arm64 linux/amd64)"
[[ "${got}" == "linux/amd64" ]] || fail "explicit NEO4J_PLATFORM=linux/amd64 overridden to '${got}'"
# --backend nornicdb never starts Neo4j, so it must not probe docker at all.
probe_marker="${seed_dir}/docker-version-called"
cat >"${seed_dir}/archbin/docker" <<EOF
#!/usr/bin/env bash
touch "${probe_marker}"
printf 'arm64\n'
EOF
got="$(env -u NEO4J_PLATFORM PATH="${seed_dir}/archbin:${PATH}" ESHU_LIVE_RUNNER_SELFTEST=1 \
	bash "${script}" --backend nornicdb 2>&1 | { rg '^neo4j_platform=' || true; } | cut -d= -f2-)"
[[ ! -e "${probe_marker}" ]] || fail "--backend nornicdb probed docker for a neo4j platform"
[[ -z "${got}" ]] || fail "--backend nornicdb resolved neo4j platform '${got}', want none"
rg -q 'platform: \$\{NEO4J_PLATFORM:-linux/amd64\}' "${repo_root}/docker-compose.live-backend-neo4j.yml" ||
	fail "neo4j live-backend compose must still honour NEO4J_PLATFORM"

# ── Image pins match the canonical compose files (no silent drift) ───────
nornicdb_compose="$(probe | rg '^nornicdb_compose=' | cut -d= -f2-)"
neo4j_compose="$(probe | rg '^neo4j_compose=' | cut -d= -f2-)"
nornicdb_pin="$(rg '^    image: ' "${repo_root}/${nornicdb_compose}" | head -n 1 | sed 's/^    image: //')"
neo4j_pin="$(rg '^    image: ' "${repo_root}/${neo4j_compose}" | head -n 1 | sed 's/^    image: //')"
[[ -n "${nornicdb_pin}" && -n "${neo4j_pin}" ]] || fail "could not read image pins from live-backend compose files"
rg --fixed-strings --quiet -- "${nornicdb_pin}" "${repo_root}/docker-compose.yaml" ||
	fail "nornicdb image pin drifted from docker-compose.yaml: ${nornicdb_pin}"
rg --fixed-strings --quiet -- "${neo4j_pin}" "${repo_root}/docker-compose.neo4j.yml" ||
	fail "neo4j image pin drifted from docker-compose.neo4j.yml: ${neo4j_pin}"

# ── Targets: one line per CI ledger row, module-relative, named tests ────
mapfile -t lines < <(python3 "${targets}" "${ledger}" "${repo_root}")
[[ "${#lines[@]}" -gt 0 ]] || fail "no CI targets extracted"
for line in "${lines[@]}"; do
	# file|package|tests|backends; the tests field itself contains |
	# separators, so only the head (go/ path) and tail (pinned backends)
	# are asserted here.
	[[ "${line}" == go/* ]] || fail "malformed target line: ${line}"
	case "${line}" in
		*\|nornicdb|*\|neo4j|*\|both) ;;
		*) fail "target line pins no valid backends: ${line}" ;;
	esac
done
ci_rows="$(python3 - "${ledger}" <<'EOF'
import re, sys
print(sum(1 for _, cls in re.findall(r"^  - file: (\S+)\n    tag: .*\n    class: (\S+)\n", open(sys.argv[1]).read(), re.M) if cls == "ci"))
EOF
)"
[[ "${#lines[@]}" == "${ci_rows}" ]] ||
	fail "target count ${#lines[@]} != CI ledger rows ${ci_rows}"

# ── Plan: every CI row schedules on its pinned backends ─────────────────
# (guards the runs-loop field split: the tests field holds |-joined
# names, so a left-anchored split silently dropped multi-Test files)
mapfile -t plan < <(ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend both)
[[ "${#plan[@]}" == "54" ]] || fail "planned runs ${#plan[@]}, want 54 (28 nornicdb + 26 neo4j)"
mapfile -t plan_nornicdb < <(ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend nornicdb)
[[ "${#plan_nornicdb[@]}" == "28" ]] || fail "nornicdb planned runs ${#plan_nornicdb[@]}, want 28"
mapfile -t plan_neo4j < <(ESHU_LIVE_RUNNER_SELFTEST=plan bash "${script}" --backend neo4j)
[[ "${#plan_neo4j[@]}" == "26" ]] || fail "neo4j planned runs ${#plan_neo4j[@]}, want 26"
printf '%s\n' "${plan_neo4j[@]}" | rg -q '^neo4j\|[^|]*\|.*ownership_neo4j_live_test' ||
	fail "directory ownership regression missing from neo4j plan"
printf '%s\n' "${plan_nornicdb[@]}" | rg -q '^nornicdb\|[^|]*\|.*ownership_neo4j_live_test' &&
	fail "neo4j-only directory ownership regression scheduled on nornicdb"
printf '%s\n' "${plan_neo4j[@]}" | rg -q '^neo4j\|[^|]*\|.*trace_deployment_oci_bound_live_test' ||
	fail "OCI registry-truth row-limit proof (#6590) missing from neo4j plan"
printf '%s\n' "${plan_nornicdb[@]}" | rg -q '^nornicdb\|[^|]*\|.*trace_deployment_oci_bound_live_test' &&
	fail "neo4j-only OCI registry-truth row-limit proof (#6590) scheduled on nornicdb"
for backend in nornicdb neo4j; do
	printf '%s\n' "${plan[@]}" | rg -q "^${backend}\\|[^|]*\\|.*context_uid_anchor_live_test" ||
		fail "entity-context uid anchor regression missing from ${backend} plan"
done
for multi in scoped_grant_live_test scoped_selector_live_test; do
	printf '%s\n' "${plan[@]}" | rg -q "^nornicdb\\|[^|]*\\|.*${multi}" || fail "${multi} missing from nornicdb plan"
	printf '%s\n' "${plan[@]}" | rg -q "^neo4j\\|[^|]*\\|.*${multi}" || fail "${multi} missing from neo4j plan"
done
for pinned in complexity_list_answer_truth nornicdb_incoming_anchor; do
	printf '%s\n' "${plan[@]}" | rg -q "^neo4j\\|[^|]*\\|.*${pinned}" &&
		fail "${pinned} scheduled on neo4j despite nornicdb pin"
done

printf 'test-run-live-backend-tests: static probes passed (%s targets, %s planned runs)\n' "${#lines[@]}" "${#plan[@]}"
