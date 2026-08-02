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

shopt -s nullglob
rule_files=("${rules_dir}"/*.md "${rules_dir}"/**/*.md)
shopt -u nullglob

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

	# Collect the glob list: `  - "pattern"` lines inside the frontmatter block.
	mapfile -t globs < <(
		awk 'NR==1&&/^---$/{infm=1;next} infm&&/^---$/{exit} infm&&/^[[:space:]]*-[[:space:]]*/{
			line=$0
			sub(/^[[:space:]]*-[[:space:]]*/,"",line)
			gsub(/^"|"$/,"",line)
			gsub(/^'"'"'|'"'"'$/,"",line)
			print line
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
