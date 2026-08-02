#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Verify every .claude/rules/*.md file is well-formed and still points at
# something real.
#
# The failure this exists for is silent. Claude Code loads a path-scoped rule
# when it reads a file matching the rule's `paths:` glob. If a directory is
# renamed and the glob stops matching, nothing fails: the rule simply never
# loads again, and the file sitting in the tree reads as coverage. That is the
# same class as the stale skill citations in #5905 -- a pointer that resolves to
# nothing while looking authoritative.
#
# Two checks, both cheap:
#
#   1. Every rule declares a `paths:` field. A rule without one loads
#      unconditionally at launch at the same priority as .claude/CLAUDE.md,
#      which is what the root canon is for. Un-scoped rules here are always a
#      mistake, and an expensive one -- they spend context in every session.
#   2. Every declared glob matches at least one tracked file.
#
# What this does NOT check: whether the rule's content is correct, or whether
# Claude actually honored it. For that, the `InstructionsLoaded` hook logs which
# instruction files loaded and why.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
rules_dir="${repo_root}/.claude/rules"

fail=0
checked=0
globs_checked=0

note() { printf '%s\n' "$*"; }
bad() {
	printf 'FAIL: %s\n' "$*" >&2
	fail=1
}

if [[ ! -d "${rules_dir}" ]]; then
	note "claude-rules-lint: no .claude/rules directory, nothing to check"
	exit 0
fi

# globstar is REQUIRED here and is off by default in bash. Without it `**`
# behaves as a single `*` and reaches exactly one level below .claude/rules, so a
# rule at .claude/rules/backend/deep/x.md is never seen and the linter exits 0
# reporting nothing to check. Claude Code discovers rule files recursively, so a
# nested rule is legitimate and skipping it is precisely the silent bypass this
# script exists to prevent.
#
# With globstar, `**/*.md` already covers files directly in rules_dir as well as
# nested ones, so listing `*.md` separately would double-count every top-level
# rule.
shopt -s nullglob globstar
rule_files=("${rules_dir}"/**/*.md)
shopt -u nullglob globstar

if ((${#rule_files[@]} == 0)); then
	note "claude-rules-lint: .claude/rules exists but holds no .md files"
	exit 0
fi

for rule in "${rule_files[@]}"; do
	rel="${rule#"${repo_root}"/}"
	checked=$((checked + 1))

	# Frontmatter must be the very first line; Claude Code does not scan for it.
	if [[ "$(head -n 1 "${rule}")" != "---" ]]; then
		bad "${rel}: no YAML frontmatter on line 1, so \`paths:\` is not parsed and the rule loads at launch"
		continue
	fi

	if ! rg -q '^paths:' "${rule}"; then
		bad "${rel}: no \`paths:\` field — this rule loads unconditionally in every session"
		continue
	fi

	# Collect the glob list from the `paths:` block ONLY.
	#
	# The first version of this collected every `- item` line anywhere in the
	# frontmatter. A rule with an empty `paths:` followed by an unrelated list
	# (`tags:` with items under it) therefore passed, because those items were
	# read as globs -- while Claude Code saw a null `paths` and loaded the rule
	# unconditionally. The rule looked scoped and was not.
	#
	# So: enter the block at a `paths:` key, take only lines indented deeper than
	# that key, and leave the block at the next key at the same or shallower
	# indentation. Flow style (`paths: ["a", "b"]`) is handled on the key line
	# itself, since it is valid YAML that Claude Code accepts.
	mapfile -t globs < <(
		awk '
		NR==1 && /^---$/ { infm=1; next }
		infm && /^---$/ { exit }
		!infm { next }
		{
			indent = match($0, /[^ \t]/) - 1
			if ($0 ~ /^[ \t]*paths[ \t]*:/) {
				inpaths = 1
				paths_indent = indent
				rest = $0
				sub(/^[ \t]*paths[ \t]*:[ \t]*/, "", rest)
				# Flow style: pull each quoted or bare item out of the brackets.
				if (rest ~ /^\[/) {
					gsub(/^\[|\][ \t]*$/, "", rest)
					n = split(rest, parts, ",")
					for (i = 1; i <= n; i++) {
						item = parts[i]
						gsub(/^[ \t]+|[ \t]+$/, "", item)
						gsub(/^"|"$/, "", item)
						gsub(/^'"'"'|'"'"'$/, "", item)
						if (item != "") print item
					}
					inpaths = 0
				}
				next
			}
			if (!inpaths) next
			# A key at the same or shallower indentation ends the paths block.
			if ($0 ~ /^[ \t]*[A-Za-z_][A-Za-z0-9_-]*[ \t]*:/ && indent <= paths_indent) {
				inpaths = 0
				next
			}
			if ($0 ~ /^[ \t]*-[ \t]*/ && indent > paths_indent) {
				item = $0
				sub(/^[ \t]*-[ \t]*/, "", item)
				gsub(/[ \t]+$/, "", item)
				gsub(/^"|"$/, "", item)
				gsub(/^'"'"'|'"'"'$/, "", item)
				if (item != "") print item
			}
		}' "${rule}"
	)

	if ((${#globs[@]} == 0)); then
		bad "${rel}: \`paths:\` present but no patterns under it"
		continue
	fi

	for glob in "${globs[@]}"; do
		globs_checked=$((globs_checked + 1))
		# A literal '[' that is not a bracket expression makes a glob match
		# nothing in Claude Code, silently. Flag it rather than let it rot.
		if [[ "${glob}" == *'['* && "${glob}" != *'\['* ]]; then
			bad "${rel}: glob '${glob}' contains an unescaped '[' — glob syntax reads it as a bracket expression"
			continue
		fi
		# `:(glob)` is required. Without it git pathspec uses its default
		# matching, where `*` crosses `/` and `**` has no special meaning, so
		# every `a/**/*.go` pattern reports zero matches and this check fails
		# against correct globs. With it, git uses wildmatch, where `**` spans
		# zero or more path segments -- the same semantics Claude Code documents
		# ("src/**/*" matches all files under src/).
		if [[ -z "$(git -C "${repo_root}" ls-files -- ":(glob)${glob}" | head -n 1)" ]]; then
			bad "${rel}: glob '${glob}' matches no tracked file — the rule will never load"
		fi
	done
done

if ((fail != 0)); then
	printf '\nclaude-rules-lint: failed. A rule whose glob matches nothing never loads,\n' >&2
	printf 'and leaves a file in the tree that reads as coverage it does not provide.\n' >&2
	exit 1
fi

note "claude-rules-lint: ${checked} rule(s), ${globs_checked} glob(s), all scoped and all matching tracked files"
