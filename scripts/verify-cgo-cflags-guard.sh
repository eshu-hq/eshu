#!/usr/bin/env bash
#
# verify-cgo-cflags-guard.sh - fail when a place that compiles Go (and so cgo)
# does not carry the C dialect flag from #9066.
#
# go-sitter-forest/perl v1.9.9 does not compile as C23, and GCC 15 on glibc 2.43
# defaults to C23, so a cold Go build cache fails with
# "bsearch.h:72:7: error: expected identifier or '(' before '_Generic'".
# scripts/lib/cgo-cflags.sh holds the full reason and the removal condition.
#
# Checks:
#   1. Every .github/workflows/*.yml that provisions Go (actions/setup-go, a
#      ci-tools container) or runs go build/test/vet/run/install sets
#      CGO_CFLAGS with -std=gnu17 in its workflow-level env. GitHub passes no
#      env between workflow files, so each file must carry its own.
#   2. No CGO_CFLAGS assignment anywhere in a workflow drops -std=gnu17, so a
#      later job or step cannot clobber the flag.
#   3. Every script under scripts/ that runs `CGO_ENABLED=1 go ...` sources
#      scripts/lib/cgo-cflags.sh and calls eshu_export_cgo_cflags.
#   4. The developer entrypoints (--list-entrypoints) do the same, and the
#      Makefile exports CGO_CFLAGS with exactly -std=gnu17 on the export line.
#
# Shape table. The guard reads literal text and never evaluates it. Every
# unresolved case below fails SAFE (the guard reports a violation) unless it is
# listed under "Accepted".
#
#   Family            Workflow YAML and its run: shell      Makefile
#   ----------------- ------------------------------------- ----------------------
#   comment strip     whole-line "#"; trailing at white-    any "#" (make needs
#                     space + "#" (a quoted " #" is cut     no space); "\#" is cut
#                     from the value only, not from Go      too (fail safe)
#                     detection)
#   assignment name   CGO_CFLAGS or CGO_CPPFLAGS, optional  CGO_CFLAGS, optional
#                     quote around a YAML key, blanks or    blanks before the
#                     tabs before ":" "=" "+="              operator
#   operator          ":" "=" "+="                          ":=" "::=" ":::=" "="
#                                                           "?=" count; "+=" "!="
#                                                           do not
#   export            n/a (env key or shell var)            "export" on the same
#                                                           line, "override" ok
#   separators        value ends at ";" "&" "|" or the      value runs to the
#                     next CGO_* assignment                 end of the line
#   -std= count       exactly one, and it is -std=gnu17;    exactly one, gnu17
#                     "+=" may add anything but a -std=;
#                     CGO_CPPFLAGS may hold no -std=
#   expressions       "${{" in a value fails                n/a
#   unset spellings   unset [-v], env -u, env --unset[=],   unexport CGO_CFLAGS,
#                                                           define CGO_CFLAGS
#                     env -i, --ignore-environment,
#                     export -n, declare/typeset +x,
#                     Remove-Item Env:
#   Go detection      whole line, only whole-line comments  n/a
#                     removed
#
# Checks 3 and 4 (scripts) are line-based text checks, not a shell parser. These
# do not satisfy them: a whole-line comment, a trailing " #" comment, an echo
# string that holds none of ";", "&", "|", "(", "{", "then", "do", "else", and a
# definition written "name() {", "name () {", or "function name". Sourcing the
# library without calling the export does nothing, so both must be present.
#
# Accepted, because each needs a shell, YAML, or make parser and the rest of the
# repo's guards do not use one:
#   - scripts: a string that holds one of those words or separators (for example
#     echo "x; eshu_export_cgo_cflags"); a heredoc body line; a call inside a
#     function that nothing calls or a branch that never runs. The build helpers
#     (build_bin, ifa_det_build_bin) call the export at the top of the function
#     that runs go, and the mirror tests run the real entrypoints.
#   - workflows: CGO_CFLAGS=-O2 go build -x -std=gnu17 (a later -std= on the
#     same command counts); a value set through a YAML anchor, job-level env, or
#     "go env -w" (these fail safe: no workflow-level flag is found); a quoted
#     value that holds " #", ";", "&", or "|" (fail safe: the value is cut
#     short); a -std= that reaches the compiler by another route, such as a CC
#     value (CC: "gcc -std=gnu23") or wrapper, a #cgo directive in Go source, or
#     a ${{ }} expression in CGO_CPPFLAGS whose value is only known at run time;
#     other ways to hide the variable, such as "readonly" or an "env -S" string.
#   - Makefile: an "include" or "-include" of a file that sets CGO_CFLAGS (the
#     guard reads one file; the real Makefile includes nothing); a "$(eval ...)"
#     or target-specific "target: CGO_CFLAGS := ..." that DROPS the flag without
#     adding a -std= (adding one fails); a later assignment that resets the variable without a -std=
#     (the real Makefile has exactly that in its else branch, which keeps a
#     caller's own -std=, and telling the two apart needs make); a "CGO_CFLAGS :=
#     ... -std=gnu17" line with a separate "export CGO_CFLAGS" (fail safe); a
#     backslash continuation (fail safe).
#
# Coverage is exactly the files named above. A script that runs go without the
# `CGO_ENABLED=1 go` prefix gets the flag from a workflow, make, or one of the
# entrypoints, or from `go env -w CGO_CFLAGS=...` on the developer's host (see
# docs/public/reference/local-testing/cgo-cflags.md).
#
# Usage: scripts/verify-cgo-cflags-guard.sh [--root DIR] | --list-entrypoints
# Mirror test: scripts/test-verify-cgo-cflags-guard.sh
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${script_dir}/.." && pwd)"

# Entrypoints a developer runs directly live in a data file next to the library
# (one repo-relative path per line, "#" comments allowed). Each must source the
# library and call the export (the Makefile sets the same value itself). The
# list is data rather than literal script paths here so verify-dockerhub-login.sh,
# which follows every script path a workflow step names, does not read this
# guard as a runner of those scripts. Read from the guard's own checkout, not
# --root, so fixture trees need no copy. --entrypoints-file reads another list
# (the mirror test uses it to plant a list that lost a line).
entry_file="${script_dir}/lib/cgo-cflags-entrypoints.txt"
list_only=0

while [ $# -gt 0 ]; do
	case "$1" in
	--root)
		root="$(cd "$2" && pwd)"
		shift 2
		;;
	--entrypoints-file)
		entry_file="$2"
		shift 2
		;;
	--list-entrypoints)
		list_only=1
		shift
		;;
	*)
		printf 'usage: %s [--root DIR] [--entrypoints-file FILE] | --list-entrypoints\n' "$0" >&2
		exit 2
		;;
	esac
done

entrypoints="$(sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "${entry_file}")"
if [ "${list_only}" -eq 1 ]; then
	printf '%s\n' "${entrypoints}"
	exit 0
fi

failures=0
fail() {
	printf 'FAIL: %s\n' "$1" >&2
	failures=$((failures + 1))
}

# The list must keep these entrypoints (Makefile or a path ending in the name).
# The names are bare words here, not paths, for the same reason the list is a
# data file; a dropped line would otherwise leave the guard and its mirror green.
for required in Makefile pre-push.sh pre-pr.sh prove.sh run-selected-gates.sh \
	install-local-binaries.sh precommit-go.sh; do
	if ! printf '%s\n' "${entrypoints}" | rg -q -x -e "(.*/)?${required//./\\.}"; then
		fail "the entrypoint list ${entry_file##*/} is missing ${required}; the guard requires it (#9066)"
	fi
done

# Matches a workflow line that provisions Go or compiles with it. Comment
# lines are removed first.
go_user_pattern='actions/setup-go|container:.*ci-tools|(^|[^A-Za-z0-9_./-])go( -C [^ ]+)? (build|test|vet|run|install)([^A-Za-z0-9_-]|$)'

# code_of prints a file without whole-line comments or trailing " #" comments.
# The trailing strip is deliberately simple: a " #" inside a quoted string
# loses the rest of that line, which can only hide text, never invent a call.
code_of() {
	sed -e '/^[[:space:]]*#/d' -e 's/[[:space:]]#.*$//' "$1"
}

# A command position: start of line, or after ; & | ( { or then/do/else. It
# keeps "source" and the export name inside echo strings from counting.
cmd_pos='(^[[:space:]]*|[;&|({][[:space:]]*|(then|do|else)[[:space:]]+)'

# sources_lib succeeds when the file has a real source (or ".") command naming
# cgo-cflags.sh; exports_lib succeeds when it calls eshu_export_cgo_cflags. A
# definition "name() {" or "name () {" is not a call, and "function name" never
# sits at a command position.
sources_lib() {
	code_of "$1" | rg -q -e "${cmd_pos}(source|\\.)[[:space:]]+[^#]*cgo-cflags\\.sh"
}
exports_lib() {
	code_of "$1" \
		| rg -e "${cmd_pos}eshu_export_cgo_cflags([^[:alnum:]_]|\$)" \
		| rg -v -e 'eshu_export_cgo_cflags[[:space:]]*\(' >/dev/null
}

# require_lib_use REL describes what is missing for the script at ROOT/REL.
require_lib_use() {
	local rel="$1" why="$2"
	if ! sources_lib "${root}/${rel}"; then
		fail "${rel} ${why} but has no non-comment line that sources scripts/lib/cgo-cflags.sh (#9066)"
	elif ! exports_lib "${root}/${rel}"; then
		fail "${rel} ${why} and sources scripts/lib/cgo-cflags.sh but never calls eshu_export_cgo_cflags (#9066)"
	fi
}

# cgo_cflags_assignments FILE prints one tab-separated record per CGO_CFLAGS or
# CGO_CPPFLAGS assignment, append, or unset-like command in a workflow: OK or
# BAD, "env" when it is a workflow-level env key (else "-"), and the line.
# Whole-line comments are dropped first. YAML (and the shell inside "run:")
# starts a trailing comment at whitespace plus "#", but a "#" inside a quoted
# shell string looks the same, so the comment cut is applied to the VALUE only.
# The name may be followed by blanks and may be a quoted YAML key; the operator
# is ":", "=", or "+=". An assignment is OK when the text between the operator and
# the comment start (or the next ";", "&", "|", or CGO_* assignment) holds
# -std=gnu17, no other -std=, and no "${{" expression. A "+=" append keeps the
# earlier value, so it is OK unless it adds a -std=. Any CGO_CPPFLAGS value with
# -std= is BAD, because cgo passes those flags to the C compiler as well. An
# assignment after a comment start has no checkable value, so it is BAD. These
# are BAD too: "unset [-v] CGO_CFLAGS", "env -u CGO_CFLAGS", "env --unset[=]
# CGO_CFLAGS", "env -i" (or "--ignore-environment"), and "export -n" or
# "declare +x" naming CGO_CFLAGS, and PowerShell "Remove-Item Env:CGO_CFLAGS". Every error here turns a pass into a FAIL,
# never the reverse.
cgo_cflags_assignments() {
	sed -e '/^[[:space:]]*#/d' "$1" | awk '
		function value_of(s,   e) {
			e = length(s) + 1
			if (match(s, /[;&|]/) && RSTART < e) e = RSTART
			if (match(s, /[[:space:]]CGO_[A-Z_]+[[:space:]]*\+?[:=]/) && RSTART < e) e = RSTART
			return substr(s, 1, e - 1)
		}
		BEGIN {
			sq = sprintf("%c", 39)
			q = "[\"" sq "]?"
			assign_re = "CGO_(CFLAGS|CPPFLAGS)" q "[[:space:]]*\\+?[:=]"
			env_key_re = "^[[:space:]]+" q "CGO_CFLAGS" q "[[:space:]]*:"
		}
		/^env:[[:space:]]*$/ { inenv = 1; next }
		/^[^[:space:]#]/ { inenv = 0 }
		{
			line = $0
			cstart = length(line) + 1
			if (match(line, /[[:space:]]#/)) cstart = RSTART
			if (line ~ /CGO_CFLAGS/ && (line ~ /(^|[^[:alnum:]_])unset[[:space:]]+[^;&|]*CGO_CFLAGS/ || line ~ /-u[[:space:]]*CGO_CFLAGS/ || line ~ /--unset[=[:space:]]+CGO_CFLAGS/ || line ~ /(^|[^[:alnum:]_])export[[:space:]]+-[a-zA-Z]*n/ || line ~ /(declare|typeset)[[:space:]]+\+[a-z]*x/ || line ~ /Remove-Item[[:space:]]+[^;&|]*CGO_CFLAGS/))
				printf "BAD\t-\t%s\n", line
			else if (line ~ /(^|[^[:alnum:]_])env[[:space:]]+(-[^[:space:]]+[[:space:]]+)*(-i|--ignore-environment|-)([[:space:]]|$)/)
				printf "BAD\t-\t%s\n", line
			scan = line
			off = 0
			while (match(scan, assign_re)) {
				rs = RSTART; rl = RLENGTH
				name = substr(scan, rs, rl)
				start = off + rs + rl
				val = ""
				if (off + rs < cstart && cstart > start) val = value_of(substr(line, start, cstart - start))
				rest = val
				gsub(/-std=gnu17/, "", rest)
				if (name ~ /CPPFLAGS/) {
					# cgo passes CGO_CPPFLAGS to the C compiler too: no -std= here.
					ok = (val !~ /-std=/)
				} else if (name ~ /\+/) {
					# An append keeps the earlier value, so it may add anything but
					# a later -std= (a repeated -std=gnu17 is harmless).
					ok = (rest !~ /-std=/) && (val !~ /\$\{\{/)
				} else {
					ok = (val ~ /-std=gnu17/) && (val !~ /\$\{\{/) && (rest !~ /-std=/)
				}
				kind = (inenv && name !~ /CPPFLAGS/ && name !~ /\+/ && line ~ env_key_re && off == 0) ? "env" : "-"
				printf "%s\t%s\t%s\n", ok ? "OK" : "BAD", kind, line
				off = off + rs + rl - 1
				scan = substr(scan, rs + rl)
			}
		}
	'
}

# makefile_exports_flag prints "yes" when the Makefile has a line that exports
# CGO_CFLAGS (":=", "::=", ":::=", "=", or "?=", with or without "override")
# whose value holds exactly one -std=, and it is -std=gnu17. make starts a comment
# at any "#", with no space needed, so this strip differs from code_of's
# whitespace rule (a "\#" is cut too, which can only turn a pass into a FAIL).
# It is "no" when any other CGO_CFLAGS assignment adds a -std=, when the
# variable is unexported, or when a "define CGO_CFLAGS" block (any flavor, with
# or without "override") sets it, because the block body has no "CGO_CFLAGS="
# token to scan.
makefile_exports_flag() {
	sed -e 's/#.*$//' "$1" | awk '
		function std_count(s,   n, t) {
			n = 0; t = s
			while (match(t, /-std=/)) { n++; t = substr(t, RSTART + RLENGTH) }
			return n
		}
		/(^|[^[:alnum:]_])unexport[[:space:]]+([^;]*[[:space:]])?CGO_CFLAGS/ { bad = 1 }
		/(^|[^[:alnum:]_])define[[:space:]]+CGO_CFLAGS/ { bad = 1 }
		{
			if (!match($0, /CGO_CFLAGS[[:space:]]*[:+?!]*=/)) next
			pre = substr($0, 1, RSTART - 1)
			if (pre ~ /[[:alnum:]_]$/) next
			op = substr($0, RSTART, RLENGTH)
			sub(/^CGO_CFLAGS[[:space:]]*/, "", op)
			val = substr($0, RSTART + RLENGTH)
			exporting = (pre ~ /(^|[^[:alnum:]_])export([[:space:]]|$)/)
			plain = (op ~ /^(:|::|:::|\?)?=$/)
			if (exporting && plain && std_count(val) == 1 && val ~ /-std=gnu17/) found = 1
			else if (std_count(val) > 0) bad = 1
		}
		END { print (found && !bad) ? "yes" : "no" }
	'
}

# workflow_env_has_flag prints "yes" when the workflow-level env block (a
# column-zero "env:" key) sets CGO_CFLAGS to a value with exactly -std=gnu17.
workflow_env_has_flag() {
	cgo_cflags_assignments "$1" | awk -F '\t' '$1 == "OK" && $2 == "env" { f = 1 } END { print f ? "yes" : "no" }'
}

if [ -d "${root}/.github/workflows" ]; then
	for wf in "${root}"/.github/workflows/*.yml "${root}"/.github/workflows/*.yaml; do
		[ -f "${wf}" ] || continue
		rel="${wf#"${root}"/}"
		# Go detection sees the whole line, so a quoted "#" cannot hide a call.
		body="$(sed -e '/^[[:space:]]*#/d' "${wf}")"
		if printf '%s\n' "${body}" | rg -q -e "${go_user_pattern}"; then
			if [ "$(workflow_env_has_flag "${wf}")" != "yes" ]; then
				fail "${rel} compiles Go but has no workflow-level env CGO_CFLAGS containing -std=gnu17 (#9066)"
			fi
		fi
		# Any assignment that lacks the flag would clobber the workflow value.
		clobbers="$(cgo_cflags_assignments "${wf}" | awk -F '\t' '$1 == "BAD" { print $3 }')"
		if [ -n "${clobbers}" ]; then
			fail "${rel} assigns CGO_CFLAGS without -std=gnu17 (#9066): $(printf '%s\n' "${clobbers}" | head -1)"
		fi
	done
fi

if [ -d "${root}/scripts" ]; then
	while IFS= read -r sh_file; do
		[ -n "${sh_file}" ] || continue
		case "$(basename "${sh_file}")" in
		test-* | verify-cgo-cflags-guard.sh) continue ;;
		esac
		require_lib_use "${sh_file}" "runs CGO_ENABLED=1 go"
	done < <(cd "${root}" && { rg -l -e '^[[:space:]]*CGO_ENABLED=1 go ' scripts --glob '*.sh' || true; } | sort)
fi

while IFS= read -r entry; do
	[ -n "${entry}" ] || continue
	if [ ! -f "${root}/${entry}" ]; then
		fail "${entry} is missing; it must carry the #9066 CGO_CFLAGS flag"
	elif [ "${entry}" = "Makefile" ]; then
		if [ "$(makefile_exports_flag "${root}/${entry}")" != "yes" ]; then
			fail "${entry} has no non-comment export of CGO_CFLAGS with exactly -std=gnu17 (#9066)"
		fi
	else
		require_lib_use "${entry}" "is a developer entrypoint"
	fi
done <<EOF
${entrypoints}
EOF

if [ "${failures}" -ne 0 ]; then
	printf '%s cgo-cflags violation(s); see scripts/lib/cgo-cflags.sh\n' "${failures}" >&2
	exit 1
fi
printf 'cgo-cflags guard: ok\n'
