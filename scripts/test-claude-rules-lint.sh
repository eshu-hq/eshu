#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Tests for scripts/dev/claude-rules-lint.sh.
#
# Each case builds a throwaway git repo, drops one rule file into it, and runs
# the linter against it. A real repo is required because the linter asks git
# which files a glob matches -- the pathspec semantics ARE the thing under test,
# and stubbing them out would test nothing. The `:(glob)` case exists because the
# first version of the linter omitted it and reported nine correct globs as
# broken.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
lint="${script_dir}/dev/claude-rules-lint.sh"

PASS=0
FAIL=0

# make_repo <rule-body> [extra-file...] -> prints the repo path
make_repo() {
	local body="$1"
	shift
	local dir
	dir="$(mktemp -d)"
	git -C "${dir}" init -q
	git -C "${dir}" config user.email t@example.com
	git -C "${dir}" config user.name t
	mkdir -p "${dir}/.claude/rules" "${dir}/scripts/dev"
	cp "${lint}" "${dir}/scripts/dev/claude-rules-lint.sh"
	printf '%s' "${body}" > "${dir}/.claude/rules/case.md"
	local f
	for f in "$@"; do
		mkdir -p "${dir}/$(dirname "${f}")"
		printf 'x\n' > "${dir}/${f}"
	done
	git -C "${dir}" add -A >/dev/null 2>&1
	printf '%s' "${dir}"
}

check() {
	local name="$1" want="$2" dir="$3"
	local got=0
	bash "${dir}/scripts/dev/claude-rules-lint.sh" >/dev/null 2>&1 || got=$?
	if [[ "${got}" == "${want}" ]]; then
		printf 'PASS  %s\n' "${name}"
		PASS=$((PASS + 1))
	else
		printf 'FAIL  %s (exit %s, want %s)\n' "${name}" "${got}" "${want}" >&2
		FAIL=$((FAIL + 1))
	fi
	rm -rf "${dir}"
}

# A `**` glob must match files sitting directly in the directory, with no
# intervening path segment. This is the case the missing `:(glob)` broke.
check "double-star matches files at depth zero" 0 \
	"$(make_repo '---
paths:
  - "go/internal/mcp/**/*.go"
---
body
' go/internal/mcp/tool.go)"

check "double-star matches files in nested dirs" 0 \
	"$(make_repo '---
paths:
  - "go/**/*.go"
---
body
' go/internal/a/b/c.go)"

# The whole point of the linter: a glob that resolves to nothing means the rule
# silently never loads.
check "glob matching nothing fails" 1 \
	"$(make_repo '---
paths:
  - "go/does/not/exist/**/*.go"
---
body
' go/internal/mcp/tool.go)"

# A rule with no paths: field loads unconditionally at launch, which is what the
# root canon is for -- never what a file in this directory should do.
check "missing paths field fails" 1 \
	"$(make_repo '# no frontmatter at all
body
' go/internal/mcp/tool.go)"

check "frontmatter present but no paths key fails" 1 \
	"$(make_repo '---
description: something
---
body
' go/internal/mcp/tool.go)"

# Frontmatter is only parsed on line 1; anywhere else it is just text.
check "frontmatter not on line 1 fails" 1 \
	"$(make_repo 'intro paragraph

---
paths:
  - "go/**/*.go"
---
body
' go/internal/mcp/tool.go)"

check "paths key with no patterns fails" 1 \
	"$(make_repo '---
paths:
---
body
' go/internal/mcp/tool.go)"

# An unescaped [ is read as a bracket expression and matches nothing, silently.
check "unescaped bracket fails" 1 \
	"$(make_repo '---
paths:
  - "photos [2024/**"
---
body
' go/internal/mcp/tool.go)"

check "multiple globs all matching passes" 0 \
	"$(make_repo '---
paths:
  - "go/**/*.go"
  - "docs/**/*.md"
---
body
' go/a.go docs/b.md)"

# One bad glob among good ones must still fail; a partially-live rule is the
# hardest kind to notice.
check "one broken glob among good ones fails" 1 \
	"$(make_repo '---
paths:
  - "go/**/*.go"
  - "nope/**/*.md"
---
body
' go/a.go)"

# --- regressions from the #5906 review -------------------------------------

# codex reproduced this: with globstar off, `**` reaches one level and a rule at
# a/b/deep.md is never seen, so the linter reports an empty directory and exits
# 0. A nested rule is legitimate -- Claude Code discovers rule files recursively.
nested_repo() {
	local dir
	dir="$(mktemp -d)"
	git -C "${dir}" init -q
	git -C "${dir}" config user.email t@example.com
	git -C "${dir}" config user.name t
	mkdir -p "${dir}/.claude/rules/a/b" "${dir}/scripts/dev" "${dir}/go/internal/mcp"
	cp "${lint}" "${dir}/scripts/dev/claude-rules-lint.sh"
	printf 'no frontmatter, must be caught\n' > "${dir}/.claude/rules/a/b/deep.md"
	printf 'x\n' > "${dir}/go/internal/mcp/tool.go"
	git -C "${dir}" add -A >/dev/null 2>&1
	printf '%s' "${dir}"
}
check "nested rule without frontmatter is still caught" 1 "$(nested_repo)"

# codex reproduced this too: `paths:` null, with a sibling list whose items were
# read as globs. The rule looked scoped to the linter and was unconditional to
# Claude Code.
check "empty paths with a sibling list fails" 1 \
	"$(make_repo '---
paths:
tags:
  - "go/**/*.go"
---
body
' go/internal/mcp/tool.go)"

# A sibling list AFTER a populated paths block must not contribute globs either.
check "sibling list after populated paths is ignored" 1 \
	"$(make_repo '---
paths:
  - "nope/**/*.go"
tags:
  - "go/**/*.go"
---
body
' go/internal/mcp/tool.go)"

# Flow style is valid YAML and Claude Code accepts it, so the linter must read it
# rather than reject a correctly-scoped rule.
check "flow-style paths list is parsed" 0 \
	"$(make_repo '---
paths: ["go/**/*.go", "docs/**/*.md"]
---
body
' go/a.go docs/b.md)"

check "flow-style paths with a broken glob fails" 1 \
	"$(make_repo '---
paths: ["go/**/*.go", "nope/**/*.md"]
---
body
' go/a.go)"

printf '\nclaude-rules-lint tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
[[ "${FAIL}" -eq 0 ]]
