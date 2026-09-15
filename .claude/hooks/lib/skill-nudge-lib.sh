#!/usr/bin/env bash
# Sourced by goal-refresh.sh and skill-nudge.sh -- not run on its own.
#
# eshu_root_path walks up from a file or directory looking for the marker only
# an Eshu checkout has, and prints the resolved root directory on success.
# skill-nudge.sh only ever needed a boolean ("am I inside an Eshu checkout?"),
# so that is what it had; goal-refresh.sh's skill-nudge scan needs the actual
# root to build "<root>/.agents/skills", which is why this now prints it
# instead of just returning 0/1. Both hooks share this one implementation
# rather than keeping a second copy that can drift -- which is exactly how
# goal-refresh.sh ended up scanning "<cwd>/.agents/skills" unconditionally and
# going silent whenever cwd was a subdirectory (e.g. "<repo>/go") or absent.
eshu_root_path() { # path
	local d="${1:-}"
	[ -n "$d" ] || return 1
	[ -d "$d" ] || d="$(dirname "$d")"
	while [ -n "$d" ] && [ "$d" != "/" ] && [ "$d" != "." ]; do
		if [ -e "$d/.agents/skills/eshu-code-review" ]; then
			printf '%s\n' "$d"
			return 0
		fi
		d="$(dirname "$d")"
	done
	return 1
}

# Boolean-only form for callers that just need the gate, not the path --
# kept so skill-nudge.sh's call site (`eshu_root "$FP" || exit 0`) needs no
# change and never sees the printed path on its stdout.
eshu_root() { # path
	eshu_root_path "$1" >/dev/null
}

# Finds project skills a goal's text names that this session has not loaded,
# reusing the marker `skill-nudge.sh` and `skill-loaded.sh` already own
# (`/tmp/claude-skill-loaded-<sid12>-<id>`, keyed on the first 12 characters
# of the session id) rather than inventing a second convention. Only a real
# directory under `.agents/skills/` counts as a named skill; an id that does
# not exist there is ordinary prose and gets no nudge.
#
# Matching is word-bounded so a goal mentioning, say,
# "golang-engineering-notes" does not fire the `golang-engineering` nudge.
# Split out of goal-refresh.sh to keep that file under the repo's 500-line cap.

goal_refresh_missing_skill_nudges() { # goal_text skills_dir sid12
	local goal_text="$1" skills_dir="$2" sid="$3"
	GOAL="${goal_text}" SKILLS_DIR="${skills_dir}" SKILL_SID="${sid}" python3 -c '
import os, re
goal = os.environ.get("GOAL", "")
skills_dir = os.environ.get("SKILLS_DIR", "")
sid = os.environ.get("SKILL_SID", "")
missing = []
try:
    ids = sorted(d for d in os.listdir(skills_dir)
                 if os.path.isdir(os.path.join(skills_dir, d)))
except Exception:
    ids = []
for skill_id in ids:
    pattern = r"(?<![A-Za-z0-9_-])" + re.escape(skill_id) + r"(?![A-Za-z0-9_-])"
    if not re.search(pattern, goal):
        continue
    marker = "/tmp/claude-skill-loaded-%s-%s" % (sid, skill_id)
    if os.path.exists(marker):
        continue
    missing.append(skill_id)
print(" ".join(missing))
' 2>/dev/null
}
