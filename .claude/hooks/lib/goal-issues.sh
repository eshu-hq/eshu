#!/usr/bin/env bash
# Sourced by issue-capture.sh, goal-continue.sh and goal-refresh.sh -- not run
# on its own.
#
# The goal issue ledger is a sidecar, `<goal file>.issues`: one line per issue
# filed during a goal. Three hooks touch it -- capture writes it, the Stop hook
# refuses DONE while it holds an open issue, the refresher restates it -- and
# they must agree on WHICH ledger belongs to a session. They agree by calling
# the one resolver here, not by each carrying a copy of the lookup. The bytes
# themselves are read and written by lib/goal-issues.py. Grammar, capture rules,
# the orphan rule and the DEFER escape: docs/internal/agent-hooks-issues.md.
#
# `goal_file_retired` lives here too. goal-continue.sh and goal-refresh.sh each
# carried a copy of it, and a third copy would be a third place for the
# leading-block rules to drift.

GI_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GI_PY="${GI_LIB_DIR}/goal-issues.py"
export GI_LIB_DIR GI_PY
# shellcheck source=.claude/hooks/lib/goal-progress.sh
. "${GI_LIB_DIR}/goal-progress.sh" || return 1

# A RETIRED candidate must not shadow an active one. `/goal done` leaves a DONE
# tombstone at the per-session path, and an existence-only lookup then selects
# it ahead of a live checkout-shared goal forever -- the session goes
# unenforced and unrefreshed with nothing saying so.
#
# Retired means: the first line that is neither blank nor a CONSENT: line is
# DONE, or is a SESSION: header whose next such line is DONE.
goal_file_retired() { # path
	local line seen_header=0 head_line
	[ -f "$1" ] || return 1
	while IFS= read -r line; do
		head_line="${line#"${line%%[![:space:]]*}"}"
		case "${head_line}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*|'') continue ;;
		esac
		case "${head_line}" in
			DONE*|done*) return 0 ;;
			SESSION:*)
				if [ "${seen_header}" -eq 0 ]; then
					seen_header=1
					continue
				fi
				return 1
				;;
			*) return 1 ;;
		esac
	done <"$1"
	return 1
}

# The ledger that belongs to a goal file.
gi_sidecar() { # goal_file
	printf '%s.issues' "$1"
}

# The session a goal file is headed for: the first line past any CONSENT lines,
# when it is a `SESSION:` header. Empty for an unheaded (owner-written) file.
# Raw line, no trim -- the same reading goal-continue.sh and goal-refresh.sh
# apply when they decide ownership.
gi_header_session() { # file
	local line
	[ -f "$1" ] || return 0
	while IFS= read -r line; do
		case "${line#"${line%%[![:space:]]*}"}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*) continue ;;
		esac
		case "${line}" in
			SESSION:*)
				line="${line#SESSION:}"
				printf '%s' "${line# }"
				;;
		esac
		return 0
	done <"$1"
}

# As above for a RETIRED file, where `/goal done` has put DONE above the header.
gi_retired_header_session() { # file
	local line
	[ -f "$1" ] || return 0
	while IFS= read -r line; do
		case "${line#"${line%%[![:space:]]*}"}" in
			[Cc][Oo][Nn][Ss][Ee][Nn][Tt]:*|DONE*|done*) continue ;;
		esac
		case "${line}" in
			SESSION:*)
				line="${line#SESSION:}"
				printf '%s' "${line# }"
				;;
		esac
		return 0
	done <"$1"
}

# The goal-file candidates, in the order both hooks look: an explicit override,
# this session's own file, the worktree's shared file, the per-user file. The
# worktree pair is skipped when there is no cwd, as the refresher does.
gi_candidates() { # sid cwd
	local sid_safe
	sid_safe="$(printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '-')"
	[ -z "${CLAUDE_GOAL_FILE:-}" ] || printf '%s\n' "${CLAUDE_GOAL_FILE}"
	if [ -n "${2:-}" ] && [ "$2" != "-" ]; then
		printf '%s\n' "$2/.claude/active-goal.${sid_safe}" "$2/.claude/active-goal"
	fi
	[ -z "${HOME:-}" ] || printf '%s\n' "${HOME}/.claude/active-goal"
}

# The ACTIVE goal file this session is held to, or return 1. It mirrors the
# hooks' lookup: the first existing candidate that is not retired (an explicit
# override is honoured as named), then the SESSION header decides ownership. A
# header naming another session means "no goal for me", NOT "try the next file"
# -- the Stop hook allows the stop there rather than falling through.
gi_resolve_goal_file() { # sid cwd
	local cand hdr
	while IFS= read -r cand; do
		[ -f "${cand}" ] || continue
		if goal_file_retired "${cand}"; then
			[ "${cand}" = "${CLAUDE_GOAL_FILE:-}" ] && return 1
			continue
		fi
		hdr="$(gi_header_session "${cand}")"
		if [ -n "${hdr}" ] && [ "${hdr}" != "$1" ]; then
			return 1
		fi
		printf '%s' "${cand}"
		return 0
	done < <(gi_candidates "$1" "$2")
	return 1
}

# The RETIRED goal file this session owns, for the Stop hook's DONE check.
# `/goal done` retires the per-session file, and the lookup passes over a
# retired file, so the hook arrives here with "no goal" and must find it again.
#
# An UNHEADED retired file counts too, at the per-session and shared worktree
# paths. The agent writes its goal file, so exempting an unheaded DONE would be an
# escape: rewrite the file without the SESSION header, put DONE first, and the
# gate never runs while capture keeps filling the sidecar. A DONE headed for
# ANOTHER session is skipped; it is that session's. The per-user
# `~/.claude/active-goal` is NOT matched when unheaded: a stale one would refuse
# the stops of every goal-less session on the machine. The owner's exit is the
# sidecar (mark a record closed or deferred) or CLAUDE_GOAL_OFF on Claude.
gi_retired_goal_file() { # sid cwd
	local cand hdr own_path shared_path sid_safe
	sid_safe="$(printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '-')"
	own_path="${2:-}/.claude/active-goal.${sid_safe}"
	shared_path="${2:-}/.claude/active-goal"
	while IFS= read -r cand; do
		[ -f "${cand}" ] || continue
		goal_file_retired "${cand}" || continue
		hdr="$(gi_retired_header_session "${cand}")"
		if [ "${cand}" = "${CLAUDE_GOAL_FILE:-}" ] || [ "${cand}" = "${own_path}" ] || [ "${hdr}" = "$1" ]; then
			printf '%s' "${cand}"
			return 0
		fi
		if [ "${cand}" = "${shared_path}" ] && [ -z "${hdr}" ]; then
			printf '%s' "${cand}"
			return 0
		fi
	done < <(gi_candidates "$1" "$2")
	return 1
}

# Live session goals in a .claude dir: every non-retired `active-goal.<x>` (never
# a sidecar, lock, or temp file) and the shared `active-goal` when it is headed
# for a session. An UNHEADED shared goal is the owner's and every session
# resolves it directly, so it is never a "lead" to guess between.
gi_lead_goal_files() { # claude_dir
	local f
	for f in "$1"/active-goal "$1"/active-goal.*; do
		[ -s "${f}" ] || continue
		case "${f##*/}" in *.issues|*.lock|*.tmp|*.tmp.*) continue ;; esac
		goal_file_retired "${f}" && continue
		if [ "${f##*/}" = "active-goal" ] && [ -z "$(gi_header_session "${f}")" ]; then
			continue
		fi
		printf '%s\n' "${f}"
	done
}

# Which ledger a tool call's new issue goes to. Sets GI_KIND and GI_LEDGER;
# returns 1 when there is no goal to attach it to (the caller stays silent).
#
#   goal    this session's own goal, or the shared one it is held to
#   lead    this session has no goal (a Muse observer, another session) and
#           exactly one live goal exists in the checkout
#   orphan  two or more live goals: guessing would pin the issue on the wrong
#           lead, so it goes to active-goal.orphan.issues and the lead's next
#           refresh claims it
#
# Zero live goals is "no goal", not an orphan: nothing would ever claim the line,
# and the next goal started here would inherit an unrelated session's issue.
# shellcheck disable=SC2034 # GI_KIND and GI_LEDGER are read by the caller
gi_capture_ledger() { # sid cwd
	local goal root dir count=0 one="" f
	GI_KIND="" GI_LEDGER=""
	if goal="$(gi_resolve_goal_file "$1" "$2")" && [ -n "${goal}" ]; then
		GI_KIND=goal
		GI_LEDGER="$(gi_sidecar "${goal}")"
		return 0
	fi
	root="$(eshu_root_path "$2")" || return 1
	dir="${root}/.claude"
	while IFS= read -r f; do
		count=$((count + 1))
		one="${f}"
	done < <(gi_lead_goal_files "${dir}")
	case "${count}" in
		0) return 1 ;;
		1)
			GI_KIND=lead
			GI_LEDGER="$(gi_sidecar "${one}")"
			;;
		*)
			GI_KIND=orphan
			GI_LEDGER="${dir}/active-goal.orphan.issues"
			;;
	esac
	return 0
}

# The Stop hook's DONE check. Prints a block decision on stdout when an issue
# filed during the goal is still open; nothing otherwise, and NOT_CHECKED on
# stderr when gh could not answer. It never reads or writes the goal's
# no-progress counter: that budget would otherwise release open issues silently.
#
# The refusal is unbounded, so it has to be loud. The consecutive-refusal count
# lives in its own file, `claude-goal-issue-refusals-<session>` in TMPDIR (not the
# nudge counter's name, and not beside the ledger, where a `active-goal.*` file
# would read as a goal): "<count> <transcript offset>". Tool calls since the
# previous refusal reset it; a missing transcript is no progress. From the third
# consecutive refusal the block reason tells the agent to escalate. Python owns
# the count; this side supplies the progress signal.
gi_done_gate() { # goal_file cwd session_id transcript
	[ -n "${1:-}" ] || return 0
	local ledger sid_safe state prior=0 offset=0 progress=0 new_offset=0 second
	ledger="$(gi_sidecar "$1")"
	[ -s "${ledger}" ] || return 0
	command -v python3 >/dev/null 2>&1 || return 0
	sid_safe="$(printf '%s' "${3:-unknown}" | tr -c 'A-Za-z0-9._-' '-')"
	state="${TMPDIR:-/tmp}/claude-goal-issue-refusals-${sid_safe}"
	if [ -f "${state}" ]; then
		read -r prior second <"${state}" 2>/dev/null || true
		offset="${second:-0}"
	fi
	case "${prior}" in '' | *[!0-9]*) prior=0 ;; esac
	{
		read -r progress
		read -r new_offset
	} < <(gp_tool_use_since "${4:-}" "${offset}")
	case "${progress}" in '' | *[!0-9]*) progress=0 ;; esac
	python3 "${GI_PY}" gate "${ledger}" --cwd "${2:--}" --goal-file "$1" \
		--state "${state}" --prior "${prior}" --progress "${progress}" \
		--offset "${new_offset}" || true
}

# The refresher's per-turn text for the goal it restates: folds any orphan
# ledger from the same .claude dir into this goal's ledger first, then lists
# what is still open. No network.
gi_restate_note() { # goal_file
	local ledger orphan
	[ -n "${1:-}" ] || return 0
	command -v python3 >/dev/null 2>&1 || return 0
	ledger="$(gi_sidecar "$1")"
	orphan="$(dirname "$1")/active-goal.orphan.issues"
	[ ! -f "${orphan}" ] || python3 "${GI_PY}" merge "${orphan}" "${ledger}" 2>/dev/null || true
	[ -s "${ledger}" ] || return 0
	python3 "${GI_PY}" list "${ledger}" 2>/dev/null || true
}
