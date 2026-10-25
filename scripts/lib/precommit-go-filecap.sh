#!/usr/bin/env bash
# shellcheck shell=bash
# The 500-line Go file cap shared by the `filecap` and `filecap-all` arms of
# scripts/dev/precommit-go.sh. Split out so that script, a grandfathered file
# that may not grow, has room for the toolchain pin (#9098). Sourced by
# precommit-go.sh, which defines `repo_root` and `note` before the first call.
# shellcheck disable=SC2154  # repo_root is set by the sourcing script.

# ---------------------------------------------------------------------------
# 500-line file cap. ONE implementation, shared by the `filecap` (changed-files,
# run by the pre-commit hook and scripts/dev/pre-pr.sh) and `filecap-all`
# (whole-tree, run by the ci-gates runner) subcommands below. They used to carry
# separate bodies and drifted: `filecap` had none of the exemptions, so a commit
# touching any long _test.go file was rejected locally while CI and `filecap-all`
# both passed it, and the only way through was a //nolint:filelength marker that
# suppresses nothing in CI. Keep the logic here, not in the case arms.
#
# The blast radius of that drift, with each path in `git ls-files
# 'go/*_test.go'` counted the way filecap_count_lines counts it
# (`awk 'END{print NR}'`): 78 of those files were over 500 lines when this was
# written, and 13 were over 1,000. Every one of them was a local-only commit
# blocker that CI would have accepted, and every marker added to get past it
# suppressed nothing anywhere. The counts are stamped rather than kept current
# on purpose: they measure the drift this arm fixed, and this comment used to
# carry a running total of all tracked _test.go files that was already stale
# both times it was written (6,114, then 6,115, against a tree holding 6,122).
# The total was never what the argument rested on — 78 and 13 are.
#
# The two arms share the per-file VERDICT but not the INPUT SET, so "parity"
# below always means "same file -> same answer", never "same set of files".
# `filecap` gets whatever the pre-commit hook stages, and the go-file-cap hook
# in .pre-commit-config.yaml declares `types: [go]` — every .go file in the
# repo, sdk/go/ and tools/ and examples/ included. `filecap-all` walks only
# `git ls-files 'go/*.go'`, and the CI plugin lints only the go/ module. Outside
# go/ the local `filecap` arm is therefore STRICTER than CI: a 501-line sdk/go
# file is rejected at commit time and no CI check would ever say so. That is
# deliberate. The 500-line cap is a repo rule (AGENTS.md), not a go/-module
# rule, and narrowing `filecap` to go/ would quietly drop it from first-party
# sdk/go and tools code. It is close to biting: the largest non-test .go outside
# go/ is sdk/go/factschema/fact_kinds.go, 493 lines when this was written. The
# cap rejects at 501 (`(( lines > 500 ))`), so that file is eight lines short of
# a local-only rejection CI would not reproduce.
# ---------------------------------------------------------------------------

# filecap_skip returns 0 for a Go path the cap intentionally does not apply to,
# mirroring the authoritative CI plugin's skip()
# (tools/golangci-lint-filelength/filelength.go): _test.go files, and any path
# with a `generated`, `vendor`, or `testdata` path SEGMENT.
#
# The plugin matches with strings.Contains(path, "/<seg>/") against an ABSOLUTE
# path, so a segment at the very start still has a separator in front of it.
# These callers pass repo-RELATIVE paths, where that leading separator is
# absent — hence the `<seg>/*` alternative beside `*/<seg>/*`. Without it a
# repo-root `testdata/`, `generated/`, or `vendor/` tree would be capped locally
# and exempt in CI. That split is latent, not live: the repo-root `testdata/`
# tree holds two .go files, both `_test.go` and both under the cap, so the
# `_test.go` rule above exempts them first, and there is no repo-root
# `generated/` or `vendor/` at all. No file exercises this today. It is here so
# the local arm cannot disagree with the plugin the day one appears, and
# scripts/test-precommit-go-filecap.sh proves each of the three alternatives
# separately. The `*/<seg>/*` form is otherwise exactly equivalent to the
# plugin's substring test: `*` in a case pattern matches `/`, and the literal
# separators anchor the segment so `generated_foo/` and `vendored/` stay capped.
filecap_skip() {
	local f="$1"
	[[ "${f}" == *_test.go ]] && return 0
	case "${f}" in
		generated/*|*/generated/*) return 0 ;;
		vendor/*|*/vendor/*) return 0 ;;
		testdata/*|*/testdata/*) return 0 ;;
	esac
	return 1
}

# filecap_count_lines counts physical lines the way the plugin's bufio.Scanner
# does: a final line with no trailing newline still counts. `wc -l` counts
# newline CHARACTERS, so on such a file it reports one fewer and the local gate
# would be one line MORE permissive than CI. awk's NR counts the trailing
# partial line, matching the plugin exactly.
filecap_count_lines() {
	awk 'END { print NR }' "$1"
}

# filecap_check_file evaluates one repo-relative Go path against the cap,
# printing the violation. Returns 1 on violation, 0 otherwise.
#
# The nolint test below is scoped to match how golangci-lint actually
# suppresses this diagnostic, because an unscoped match was laxer than CI.
#
# The plugin reports at `f.Pos()` (tools/golangci-lint-filelength/filelength.go),
# which is the file's `package` clause. golangci-lint then honours a `//nolint`
# directive on that line or in the comment block immediately above it. So a
# file-scoped suppression lives at or above `package`, and nowhere else.
#
# This previously matched `nolint:filelength` ANYWHERE in the file, so a file
# that merely MENTIONED the string in prose -- a comment explaining the marker,
# for instance -- was exempt locally and still capped in CI. That is the wrong
# direction for a hook whose entire purpose is agreeing with CI.
#
# filecap_nolint_directive narrows it two ways: the match must be a real comment
# directive rather than prose, and it must appear at or above the `package`
# clause. It is still not a parser -- it does not track `//nolint` block scoping
# inside a file -- but that gap no longer produces a local pass CI would reject,
# because a file-length diagnostic cannot be suppressed from inside the body.
# filecap_nolint_directive returns 0 when the file carries a real
# //nolint:filelength directive where golangci-lint would honour it for THIS
# diagnostic: at or above the `package` clause, which is where the plugin
# reports (`Pos: f.Pos()`).
#
# "Real directive" means the comment's TEXT begins with `nolint:` -- either as a
# trailing comment on a line (`package x //nolint:filelength`, the common form)
# or as its own comment line above `package`. Prose that merely mentions the
# string mid-sentence (`// we use //nolint:filelength elsewhere`) is not a
# directive to golangci-lint and is not one here either. That was the gap: the
# old check matched the bare string anywhere in the file, so a file explaining
# the marker was exempt locally and still capped in CI.
filecap_nolint_directive() {
	local file="$1"
	# One comment-aware pass, because both halves of this question need the same
	# state machine: where the package clause really is, and which //nolint is a
	# real directive rather than text.
	#
	# A plain `rg '^package'` matches a decoy inside a /* */ block and stops the
	# search short of the real clause, so a file carrying a valid directive gets
	# capped locally while CI exempts it. And a `//nolint` written INSIDE a block
	# comment is not a directive to the compiler or to golangci-lint, so it must
	# not exempt anything here either. Both are local-vs-CI disagreements, which
	# is the failure this gate exists to remove.
	#
	# The verdict is decided in END, not by `exit 0` in the main block: awk still
	# runs END after an exit, so an `END { exit 1 }` silently overrides it.
	awk '
		{
			line = $0; code = ""; cmt = ""; i = 1
			while (i <= length(line)) {
				two = substr(line, i, 2)
				if (inblk) { if (two == "*/") { inblk = 0; i += 2 } else i++ }
				else if (two == "/*") { inblk = 1; i += 2 }
				else if (two == "//") { cmt = substr(line, i + 2); break }
				else { code = code substr(line, i, 1); i++ }
			}
			sub(/^[ \t]+/, "", cmt)
			if (cmt ~ /^nolint:/) {
				split(cmt, parts, /[ \t]/)
				linters = substr(parts[1], 8)
				if (linters ~ /(^|,)filelength(,|$)/) { found = 1 }
			}
			sub(/^[ \t]+/, "", code)
			if (code ~ /^package[ \t]/) { pkgseen = 1; exit }
		}
		END { if (pkgseen && found) { exit 0 } exit 1 }
	' "${file}" 2>/dev/null
}

filecap_check_file() {
	local f="$1" lines
	[[ "${f}" == *.go ]] || return 0
	filecap_skip "${f}" && return 0
	[[ -f "${repo_root}/${f}" ]] || return 0
	filecap_nolint_directive "${repo_root}/${f}" && return 0
	lines="$(filecap_count_lines "${repo_root}/${f}")"
	if (( lines > 500 )); then
		note "${f}: ${lines} lines exceeds the 500-line cap (split it, or //nolint:filelength with a reason)"
		return 1
	fi
	return 0
}
