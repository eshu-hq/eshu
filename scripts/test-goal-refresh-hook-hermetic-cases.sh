#!/usr/bin/env bash
# Sourced by scripts/test-goal-refresh-hook.sh -- not run on its own.
#
# Hermeticity guard (#8278): the hook under test resolves
# $HOME/.claude/active-goal and honors $CLAUDE_GOAL_FILE/$CLAUDE_GOAL_OFF, so
# ambient session state on the machine running this suite leaks into its
# expectations. Seeded: a fake $HOME carrying an unheaded goal turned the
# suite 110 passed/7 failed quoting the seeded text; a scratch
# $CLAUDE_GOAL_FILE was honored AND overwritten (77/40, live file clobbered).
#
# Each probe below re-runs this whole suite as a subprocess with exactly one
# ambient vector seeded and asserts it still passes. The child sets
# ESHU_GOAL_REFRESH_HERMETIC_CHILD=1, which skips the probes there, so the
# nesting stops at one level. The suite header exports a per-run fake $HOME
# and unsets the ambient goal switches (the test-cursor-hooks.sh precedent),
# which is what turns these green; without it they fail.
#
# A trigger path for the agent-canon gate, and its sentinel is the last line,
# asserted by test-agent-hooks.sh along with every sibling. Append ABOVE it.

if [[ -n "${ESHU_GOAL_REFRESH_HERMETIC_CHILD:-}" ]]; then
	goal_refresh_hermetic_cases_loaded=1
	return 0
fi

hermetic_child() { # log_name env... -> exit of a one-level suite rerun
	local _log="${work}/hermetic-$1.log"
	shift
	env "$@" ESHU_GOAL_REFRESH_HERMETIC_CHILD=1 \
		bash "${repo_root}/scripts/test-goal-refresh-hook.sh" >"${_log}" 2>&1
}

# A clean empty HOME for the probes that are NOT testing $HOME, so each probe
# seeds exactly one vector whatever the machine outside holds.
hermetic_clean_home="${work}/seed-clean-home"
mkdir -p "${hermetic_clean_home}/.claude"

# M1: an ambient $HOME goal. Unheaded -- the owner's hand-written form -- so
# any case quoting goal text would quote THIS before the fix.
hermetic_seed_home="${work}/seed-home"
mkdir -p "${hermetic_seed_home}/.claude"
printf 'Seeded ambient objective that must never surface.\n' >"${hermetic_seed_home}/.claude/active-goal"
if hermetic_child "home" -u CLAUDE_GOAL_FILE -u CLAUDE_GOAL_OFF -u CLAUDE_GOAL_CONSENT \
	-u CLAUDE_GOAL_MAX_NUDGES "HOME=${hermetic_seed_home}"; then
	ok "hermetic: a seeded \$HOME goal does not change the suite result"
else
	no "hermetic: a seeded \$HOME goal does not change the suite result ($(tail -1 "${work}/hermetic-home.log" 2>/dev/null))"
fi

# M2: an ambient $CLAUDE_GOAL_FILE. Honored (cases resolve it) and overwritten
# (a /goal write targets it via owns_goal_file) before the fix.
hermetic_seed_file="${work}/seed-goal-file"
printf 'PRE-EXISTING SCRATCH CONTENT THAT MUST SURVIVE\n' >"${hermetic_seed_file}"
if hermetic_child "file" -u CLAUDE_GOAL_OFF -u CLAUDE_GOAL_CONSENT -u CLAUDE_GOAL_MAX_NUDGES \
	"HOME=${hermetic_clean_home}" "CLAUDE_GOAL_FILE=${hermetic_seed_file}"; then
	ok "hermetic: a seeded \$CLAUDE_GOAL_FILE does not change the suite result"
else
	no "hermetic: a seeded \$CLAUDE_GOAL_FILE does not change the suite result ($(tail -1 "${work}/hermetic-file.log" 2>/dev/null))"
fi
if [[ "$(cat "${hermetic_seed_file}" 2>/dev/null)" == "PRE-EXISTING SCRATCH CONTENT THAT MUST SURVIVE" ]]; then
	ok "hermetic: the seeded \$CLAUDE_GOAL_FILE is left untouched"
else
	no "hermetic: the seeded \$CLAUDE_GOAL_FILE was overwritten by the suite"
fi

# The kill switch: an ambient CLAUDE_GOAL_OFF=1 silences every hook, turning
# every block-expecting case into an allow.
if hermetic_child "off" -u CLAUDE_GOAL_FILE -u CLAUDE_GOAL_CONSENT -u CLAUDE_GOAL_MAX_NUDGES \
	"HOME=${hermetic_clean_home}" "CLAUDE_GOAL_OFF=1"; then
	ok "hermetic: a seeded \$CLAUDE_GOAL_OFF does not change the suite result"
else
	no "hermetic: a seeded \$CLAUDE_GOAL_OFF does not change the suite result ($(tail -1 "${work}/hermetic-off.log" 2>/dev/null))"
fi

# LAST line on purpose -- see the sibling companions.
goal_refresh_hermetic_cases_loaded=1
