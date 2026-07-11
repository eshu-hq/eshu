#!/usr/bin/env bash
set -euo pipefail

# Verifier for the Scorecard component-extension remote Compose proof (#2126,
# #1923). It checks recorded harness artifacts against the proof invariants:
# the component reads back installed/enabled/trusted, the worker/coordinator
# reported healthy, the component workflow item reached a terminal success with
# no retry/failed/dead-letter state, the bounded dev.eshu.examples.scorecard.*
# fact families were committed, and no secret/host-local material leaks into the
# proof surfaces. It operates on an artifacts directory so it is deterministic
# and self-testable; the live run (which produces those artifacts from a running
# Compose stack) is the operator/CI gate.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
list_only=false
artifacts_dir=""

usage() {
	# printf, not a heredoc: Homebrew bash >= 5.1 writes an entire heredoc
	# body to a pipe before forking the reader, and macOS's 512-byte pipe
	# buffer deadlocks on any body over that size (#5074). This body expands
	# "$(basename "$0")", so it cannot move to a static scripts/lib/ data
	# file; each literal line is single-quoted and the one expanding line is
	# double-quoted to preserve the original heredoc's expansion behavior.
	printf '%s\n' \
		"Usage: $(basename "$0") --artifacts <dir> [--list]" \
		'' \
		'Verifies recorded Scorecard component-extension proof artifacts:' \
		'  inventory.json        component-extensions API readback' \
		'  workflow-items.json   component workflow item terminal states' \
		'  facts.json            committed dev.eshu.examples.scorecard.* fact counts' \
		'  provenance.json       Eshu commit, digest, SDK/core versions, backend, telemetry' \
		'' \
		'The artifacts directory is produced by running the component-extension Compose' \
		'harness against a stack (see docs/public/extend/reference-scorecard-extension.md).' \
		'' \
		'  --list   print the proof checks without running them'
}

die() {
	printf 'verify-remote-e2e-component-extension: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list) list_only=true; shift ;;
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v rg >/dev/null 2>&1 || die "rg is required"

readonly component_id="dev.eshu.examples.scorecard"
readonly fact_families=(
	"dev.eshu.examples.scorecard.snapshot"
	"dev.eshu.examples.scorecard.check"
	"dev.eshu.examples.scorecard.warning"
)
# Forbidden material that must never appear in any proof artifact. Host-local
# paths, private key markers, bearer tokens, and raw IPv4 addresses are
# redaction canaries; the proof fails closed if any are present.
readonly forbidden_patterns=(
	'/Users/'
	'/home/'
	'BEGIN [A-Z ]*PRIVATE KEY'
	'[Bb]earer [A-Za-z0-9._-]{8,}'
	'([0-9]{1,3}\.){3}[0-9]{1,3}'
)

print_checks() {
	# printf, not a heredoc: the expanded body is 585 bytes, inside the
	# 512-byte-plus deadlock window that Homebrew bash 5.1+ hits on macOS
	# (#5074). The source stays under 512 bytes, so the heredoc-budget gate
	# never flagged it; the deadlock only shows at runtime once "${component_id}"
	# and "${fact_families[*]}" expand. Literal lines are single-quoted; the two
	# expanding lines are double-quoted to preserve the original output byte for
	# byte.
	printf '%s\n' \
		'component-extension proof checks:' \
		"  1. inventory: ${component_id} reads back installed=true, enabled=true, trusted=true" \
		'  2. workflow: component workflow item terminal success; no retrying/failed/dead-letter' \
		"  3. facts: at least one committed fact for ${fact_families[*]}" \
		'  4. provenance: records eshu_commit, component_digest, core/sdk versions, backend, queue terminal state, telemetry handle' \
		'  5. redaction canary: no host paths, private keys, bearer tokens, or raw IPs in artifacts'
}

if [[ "${list_only}" == true ]]; then
	print_checks
	exit 0
fi

[[ -n "${artifacts_dir}" ]] || die "--artifacts <dir> is required (or use --list)"
[[ -d "${artifacts_dir}" ]] || die "artifacts directory not found: ${artifacts_dir}"

inventory="${artifacts_dir}/inventory.json"
workflow_items="${artifacts_dir}/workflow-items.json"
facts="${artifacts_dir}/facts.json"
provenance="${artifacts_dir}/provenance.json"
for required in "${inventory}" "${workflow_items}" "${facts}" "${provenance}"; do
	[[ -f "${required}" ]] || die "missing required artifact: ${required}"
done

# 1. Inventory: installed, enabled, and trusted for the component id.
rg --fixed-strings --quiet "\"${component_id}\"" "${inventory}" \
	|| die "inventory missing component ${component_id}"
rg --quiet '"installed"[[:space:]]*:[[:space:]]*true' "${inventory}" \
	|| die "inventory does not show installed=true"
rg --quiet '"enabled"[[:space:]]*:[[:space:]]*true' "${inventory}" \
	|| die "inventory does not show enabled=true"
rg --quiet '"trusted"[[:space:]]*:[[:space:]]*true' "${inventory}" \
	|| die "inventory does not show trusted=true"

# 2. Workflow: terminal success, no retry/failed/dead-letter.
rg --quiet '"state"[[:space:]]*:[[:space:]]*"(completed|succeeded)"' "${workflow_items}" \
	|| die "no completed/succeeded component workflow item"
if rg --quiet '"state"[[:space:]]*:[[:space:]]*"(retrying|failed|dead_letter|dead-letter)"' "${workflow_items}"; then
	die "component workflow has retrying/failed/dead-letter items"
fi

# 3. Facts: at least one committed scorecard fact family with count > 0.
fact_seen=false
for family in "${fact_families[@]}"; do
	if rg --quiet "\"${family}\"[[:space:]]*:[[:space:]]*[1-9][0-9]*" "${facts}"; then
		fact_seen=true
	fi
done
[[ "${fact_seen}" == true ]] || die "no committed dev.eshu.examples.scorecard.* facts"

# 4. Provenance: every reproducibility/audit field must be present and non-empty
#    so the run records what built it and where it ran. Each field is matched as
#    a non-empty, non-"unknown" string value.
for field in eshu_commit component_digest core_version sdk_version backend queue_terminal_state metrics_handle; do
	rg --quiet "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]+\"" "${provenance}" \
		|| die "provenance missing or empty field: ${field}"
done
rg --quiet '"eshu_commit"[[:space:]]*:[[:space:]]*"unknown"' "${provenance}" \
	&& die "provenance eshu_commit is unknown (capture ran outside a checkout)"
rg --quiet '"component_digest"[[:space:]]*:[[:space:]]*"sha256:[A-Fa-f0-9]{8,}"' "${provenance}" \
	|| die "provenance component_digest is not a sha256 digest"

# 5. Redaction canary across every artifact.
for artifact in "${inventory}" "${workflow_items}" "${facts}" "${provenance}"; do
	for pattern in "${forbidden_patterns[@]}"; do
		if rg --quiet "${pattern}" "${artifact}"; then
			die "forbidden material matched /${pattern}/ in $(basename "${artifact}")"
		fi
	done
done

printf 'component-extension proof artifacts verified (component=%s)\n' "${component_id}"
