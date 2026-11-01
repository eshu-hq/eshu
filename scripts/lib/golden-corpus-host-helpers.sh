#!/usr/bin/env bash
#
# golden-corpus-host-helpers.sh — host-binary build/launch/query helpers for the
# golden corpus gate orchestrator (scripts/verify-golden-corpus-gate.sh).
# Extracted into a lib chunk so the orchestrator stays under the 500-line cap.
#
# Requires (set by the orchestrator before any of these are called): bin_dir,
# log_dir, use_compose, compose_file, ESHU_POSTGRES_DSN, bg_pids (array), and
# the die() function. Bodies resolve these lazily at call time, so sourcing
# this file before those globals are set is safe as long as they exist by the
# time a given helper actually runs.

# #9066: cgo needs -std=gnu17 on GCC 15 + glibc 2.43; see cgo-cflags.sh.
# shellcheck source=cgo-cflags.sh
# (The literal scripts/ path lets the ci-gates trigger walk see this dependency.)
source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib/cgo-cflags.sh"
# shellcheck source=golden-corpus-git.sh
# All gate git traffic runs through golden_corpus_git (sanitized env/config),
# including the satellite clone below.
source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib/golden-corpus-git.sh"

# ESHU_MCP_REF pins the eshu-mcp satellite release whose serving binary the
# gates run after the in-tree removal (#9670). Tag, not a branch: reproducible.
: "${ESHU_MCP_REF:=v0.2.5}"

# build_satellite_bin clones the satellite at ESHU_MCP_REF and builds
# ./cmd/eshu-mcp as ${bin_dir}/eshu-mcp-server. The satellite replaced the
# deleted in-tree go/cmd/mcp-server; it honors the same ESHU_MCP_* env contract
# except the listen address and core origin, which moved to the -addr and
# -core-base-url flags the gate start lines pass explicitly.
build_satellite_bin() {
	local src_dir="${work_dir}/eshu-mcp-src"
	rm -rf "${src_dir}"
	golden_corpus_git clone --depth 1 --branch "${ESHU_MCP_REF}" https://github.com/eshu-hq/eshu-mcp.git "${src_dir}" \
		|| die "clone eshu-mcp ${ESHU_MCP_REF} failed"
	# Record the resolved commit: tags are mutable, so the gate log pins the
	# exact satellite tree the serving binary was built from.
	echo "eshu-mcp ${ESHU_MCP_REF} resolved to $(git -C "${src_dir}" rev-parse HEAD)"
	CGO_ENABLED=0 go -C "${src_dir}" build -trimpath -o "${bin_dir}/eshu-mcp-server" ./cmd/eshu-mcp \
		|| die "build satellite mcp-server ${ESHU_MCP_REF} failed"
}

build_bin() {
	local cmd="$1"
	if [[ "${cmd}" == "mcp-server" ]]; then
		build_satellite_bin
		return
	fi
	eshu_export_cgo_cflags
	CGO_ENABLED=1 go -C go build -o "${bin_dir}/eshu-${cmd}" "./cmd/${cmd}" \
		|| die "build ${cmd} failed"
}

# start_bg <name> <pidvar> <cmd...> launches cmd in the background, records its
# pid in bg_pids (so the cleanup trap can reap it on EVERY exit path), and writes
# the pid into the caller-named variable pidvar. The pid is assigned via
# printf -v in the PARENT shell — a previous version returned it through command
# substitution, whose subshell discarded the bg_pids append, leaving the trap a
# no-op that leaked processes on failure.
start_bg() {
	local name="$1" pidvar="$2"; shift 2
	"$@" >"${log_dir}/${name}.log" 2>&1 &
	local pid=$!
	bg_pids+=("${pid}")
	printf -v "${pidvar}" '%s' "${pid}"
}

# wait_for_gate_http <failure-message> <url> <log-name> polls url until it
# answers, dying with the service log tail otherwise. Extracted from the
# orchestrator's eshu-api /readyz and eshu-mcp-server /health waits to keep
# that file under the 500-line cap; the failure messages stay identical.
wait_for_gate_http() {
	local failure_message="$1" url="$2" log_name="$3" ready=false
	for _ in $(seq 1 30); do
		if curl -fsS "${url}" >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	[[ "${ready}" == "true" ]] || { tail -30 "${log_dir}/${log_name}.log" >&2 || true; die "${failure_message}"; }
}

# pg runs a single-value SQL query against the gate's Postgres, working in both
# compose mode (via the postgres container) and --no-compose mode (via a local
# psql client). Used to assert the cassette collectors actually committed.
# Values ride in the statement text (quote-doubled by the caller when needed):
# psql -c never interpolates :variables, so pg takes no -v pairs.
pg() {
	local sql="$1"
	if [[ "${use_compose}" -eq 1 ]]; then
		docker compose "${compose_args[@]}" exec -T postgres \
			psql -v ON_ERROR_STOP=1 -U eshu -d eshu -tA -c "${sql}"
	else
		command -v psql >/dev/null 2>&1 || die "psql client required in --no-compose mode"
		psql "${ESHU_POSTGRES_DSN}" -v ON_ERROR_STOP=1 -tA -c "${sql}"
	fi
}

# Round-4 review P3-2 (#6782): bind the log to the tested commit so a gate
# log is self-identifying without relying on timestamps plus the author's run
# record. Lives in this sourced lib, not the orchestrator, to keep that file
# under the 500-line cap; log() is defined before either sourcing script
# sources this file. PR #6867 review (codex P2): this lib is also sourced by
# verify-read-api-latency-gate.sh, which can measure a DIFFERENT checkout
# (GATE_STACK_DIR) and binary (GATE_API_BIN) than the controller checkout this
# rev-parse runs in — so name the measured artifacts explicitly instead of
# letting the controller SHA pose as the tested identity. Unset in the
# golden-corpus gate, where HEAD is the tested commit.
log "tested commit: $(git rev-parse HEAD)${GATE_STACK_DIR:+ (measured stack: ${GATE_STACK_DIR})}${GATE_API_BIN:+ (measured api binary: ${GATE_API_BIN})}"
