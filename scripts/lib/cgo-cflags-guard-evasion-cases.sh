#!/usr/bin/env bash
#
# cgo-cflags-guard-evasion-cases.sh - the evasion half of the #9066 guard mirror
# test, split out of scripts/test-verify-cgo-cflags-guard.sh to keep that file
# under the 500-line cap. It is sourced, not run: it needs new_tree,
# good_workflow, and check_guard from the mirror test, and defines
# run_evasion_cases, which plants one evasion per case (trailing comments,
# quoted "#", second -std=, unset spellings, key spellings, Makefile shapes) and
# expects the guard to FAIL it, plus the compliant spellings that must pass.

run_evasion_cases() {
	# A trailing YAML comment that names -std=gnu17 is not a value. The line
	# keeps its flag-less value, so the guard must still FAIL it; a compliant
	# value that merely ends in a comment must still pass.
	t="$(new_tree)"
	good_workflow | sed 's/CGO_CFLAGS: "-O2 -g -std=gnu17"/CGO_CFLAGS: "-O2 -g" # -std=gnu17 kept for docs/' >"${t}/.github/workflows/comment-env.yml"
	check_guard "guard: workflow-level CGO_CFLAGS with -std=gnu17 only in a trailing comment FAILS" 1 "$t" "comment-env.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: go test ./...\n        env:\n          CGO_CFLAGS: "-O2" # -std=gnu17 kept for docs\n'; } >"${t}/.github/workflows/comment-step.yml"
	check_guard "guard: step-level CGO_CFLAGS with -std=gnu17 only in a trailing comment FAILS" 1 "$t" "comment-step.yml"

	t="$(new_tree)"
	good_workflow | sed 's/CGO_CFLAGS: "-O2 -g -std=gnu17"/CGO_CFLAGS: "-O2 -g -std=gnu17" # keep/' >"${t}/.github/workflows/comment-ok.yml"
	check_guard "guard: compliant CGO_CFLAGS with a trailing comment passes" 0 "$t"

	# A "#" inside a quoted shell string is not a comment. It must neither hide
	# a Go build nor hide a CGO_CFLAGS assignment that follows it on the line.
	t="$(new_tree)"
	cat >"${t}/.github/workflows/quoted-go.yml" <<'EOF'
name: Fixture
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "build #1" && go build ./...
EOF
	check_guard "guard: Go build after a quoted # in a workflow with no env FAILS" 1 "$t" "quoted-go.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: echo "step #2"; CGO_CFLAGS=-O3 go build ./...\n'; } >"${t}/.github/workflows/quoted-clobber.yml"
	check_guard "guard: CGO_CFLAGS=-O3 after a quoted # FAILS" 1 "$t" "quoted-clobber.yml"

	# Cheap hardening: the value must be exactly the flag, in that assignment.
	t="$(new_tree)"
	good_workflow | sed 's/-std=gnu17"/-std=gnu17 -std=gnu23"/' >"${t}/.github/workflows/two-std.yml"
	check_guard "guard: a later -std=gnu23 in the value FAILS" 1 "$t" "two-std.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: env -u CGO_CFLAGS go build ./...\n'; } >"${t}/.github/workflows/env-unset.yml"
	check_guard "guard: env -u CGO_CFLAGS FAILS" 1 "$t" "env-unset.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: unset CGO_CFLAGS; go build ./...\n'; } >"${t}/.github/workflows/unset.yml"
	check_guard "guard: unset CGO_CFLAGS FAILS" 1 "$t" "unset.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: CGO_CFLAGS=-O2 CGO_CPPFLAGS=-std=gnu17 go build ./...\n'; } >"${t}/.github/workflows/cppflags.yml"
	check_guard "guard: -std=gnu17 only in CGO_CPPFLAGS on the same line FAILS" 1 "$t" "cppflags.yml"

	t="$(new_tree)"
	good_workflow | awk '{ print } /^  CGO_CFLAGS:/ { print "  CGO_CPPFLAGS: \"-std=gnu23\"" }' >"${t}/.github/workflows/cppflags-key.yml"
	check_guard "guard: a separate CGO_CPPFLAGS key with -std= FAILS" 1 "$t" "cppflags-key.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: export CGO_CFLAGS+=" -std=gnu23"\n'; } >"${t}/.github/workflows/append-std.yml"
	check_guard "guard: CGO_CFLAGS+= that adds a later -std= FAILS" 1 "$t" "append-std.yml"

	t="$(new_tree)"
	{ good_workflow; printf '      - run: export CGO_CFLAGS+=" -Wall"\n'; } >"${t}/.github/workflows/append-ok.yml"
	check_guard "guard: CGO_CFLAGS+= that keeps the workflow flag passes" 0 "$t"

	t="$(new_tree)"
	good_workflow | sed "s/CGO_CFLAGS: \"-O2 -g -std=gnu17\"/CGO_CFLAGS: \${{ vars.CFLAGS || '-std=gnu17' }}/" >"${t}/.github/workflows/expr.yml"
	check_guard "guard: a \${{ }} value that names the flag as text FAILS" 1 "$t" "expr.yml"

	# Review of #9105: spelling and unset families the first pass missed. Each
	# row appends the lines in $4 (printf %b) to the compliant workflow.
	append_case() {
		t="$(new_tree)"
		{ good_workflow; printf '%b' "$4"; } >"${t}/.github/workflows/$1"
		check_guard "$3" "$2" "$t" "$1"
	}
	step_env='      - run: go test ./...\n        env:\n'
	append_case unset-long.yml 1 "guard: env --unset=CGO_CFLAGS FAILS" '      - run: env --unset=CGO_CFLAGS go build ./...\n'
	append_case unset-long-sp.yml 1 "guard: env --unset CGO_CFLAGS FAILS" '      - run: env --unset CGO_CFLAGS go build ./...\n'
	append_case unset-v.yml 1 "guard: unset -v CGO_CFLAGS FAILS" '      - run: unset -v CGO_CFLAGS; go build ./...\n'
	append_case env-i.yml 1 "guard: env -i FAILS" '      - run: env -i PATH="$PATH" go build ./...\n'
	append_case export-n.yml 1 "guard: export -n CGO_CFLAGS FAILS" '      - run: export -n CGO_CFLAGS; go build ./...\n'
	append_case remove-item.yml 1 "guard: Remove-Item Env:CGO_CFLAGS FAILS" '      - run: Remove-Item Env:CGO_CFLAGS\n'
	append_case declare-x.yml 1 "guard: declare +x CGO_CFLAGS FAILS" '      - run: declare +x CGO_CFLAGS; go build ./...\n'
	append_case typeset-x.yml 1 "guard: typeset +x CGO_CFLAGS FAILS" '      - run: typeset +x CGO_CFLAGS; go build ./...\n'
	append_case env-ignore.yml 1 "guard: env --ignore-environment FAILS" '      - run: env --ignore-environment go build ./...\n'
	append_case env-dash.yml 1 "guard: bare env - FAILS" '      - run: env - go build ./...\n'
	append_case space-colon.yml 1 "guard: step-level 'CGO_CFLAGS : -O2' FAILS" "${step_env}"'          CGO_CFLAGS : -O2\n'
	append_case tab-colon.yml 1 "guard: step-level CGO_CFLAGS with a tab after the colon FAILS" "${step_env}"'          CGO_CFLAGS:\t-O2\n'
	append_case quoted-key.yml 1 "guard: step-level quoted key \"CGO_CFLAGS\" FAILS" "${step_env}"'          "CGO_CFLAGS": -O2\n'
	append_case squoted-key.yml 1 "guard: step-level single-quoted key 'CGO_CFLAGS' FAILS" "${step_env}""          'CGO_CFLAGS': -O2\\n"

	t="$(new_tree)"
	good_workflow | sed 's/^  CGO_CFLAGS:/  CGO_CFLAGS :/' >"${t}/.github/workflows/env-space-colon.yml"
	check_guard "guard: workflow-level 'CGO_CFLAGS :' with the flag passes" 0 "$t"

	# Line anchoring: a prefix must not be spoofed into the workflow-level env or
	# the global Makefile export.
	t="$(new_tree)"
	printf 'name: F\non: push\nenv:\n  FOO:\n    CGO_CFLAGS: "-O2 -g -std=gnu17"\njobs:\n  b:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-go@v6\n' >"${t}/.github/workflows/nested-env.yml"
	check_guard "guard: CGO_CFLAGS nested under another env key FAILS" 1 "$t" "nested-env.yml"

	t="$(new_tree)"
	good_workflow | sed 's/^env:/env: # note/' >"${t}/.github/workflows/env-comment.yml"
	check_guard "guard: workflow env: with a trailing comment passes" 0 "$t"

	t="$(new_tree)"
	good_workflow | sed 's/^env:/env:#note/' >"${t}/.github/workflows/env-nospace-comment.yml"
	check_guard "guard: workflow env:#note without a space before the # does not count as the env key, FAILS" 1 "$t" "env-nospace-comment.yml"

	# Makefile: make strips "#" with no space before it, and the last -std= wins.
	mk_case() {
		t="$(new_tree)"
		printf '%b' "$3" >"${t}/Makefile"
		check_guard "guard: Makefile $1" "$2" "$t" "$([ "$2" = 1 ] && printf Makefile)"
	}
	mk_case "with a later -std=gnu23 FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17 -std=gnu23\n'
	mk_case "with -std=gnu17 after a bare # FAILS" 1 'export CGO_CFLAGS := -O2 -g# -std=gnu17 kept for docs\n'
	mk_case "that appends a later -std= FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\nexport CGO_CFLAGS += -std=gnu23\n'
	mk_case "that unexports CGO_CFLAGS FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\nunexport CGO_CFLAGS\n'
	mk_case "that never exports CGO_CFLAGS FAILS" 1 'CGO_CFLAGS ?= -O2 -g -std=gnu17\n'
	mk_case "that sets CGO_CFLAGS with != FAILS" 1 'export CGO_CFLAGS != echo -std=gnu17\n'
	mk_case "with only a target-specific export FAILS" 1 'build: export CGO_CFLAGS := -O2 -g -std=gnu17\n'
	mk_case "with only a recipe-line export FAILS" 1 '\texport CGO_CFLAGS := -O2 -g -std=gnu17\n'
	mk_case "with only an \$(eval export) FAILS" 1 '$(eval export CGO_CFLAGS := -O2 -g -std=gnu17)\n'
	mk_case "spelling export override passes" 0 'export override CGO_CFLAGS := -O2 -g -std=gnu17\n'
	mk_case "spelling ::= passes" 0 'export CGO_CFLAGS ::= -O2 -g -std=gnu17\n'
	mk_case "spelling ?= passes" 0 'export CGO_CFLAGS ?= -O2 -g -std=gnu17\n'
	mk_case "spelling override export passes" 0 'override export CGO_CFLAGS := -O2 -g -std=gnu17\n'
	mk_case "spelling export after leading spaces passes" 0 '  export CGO_CFLAGS := -O2 -g -std=gnu17\n'
	mk_case "spelling with tabs passes" 0 'export\tCGO_CFLAGS\t:=\t-O2 -g -std=gnu17\n'
	mk_case "spelling with a trailing comment passes" 0 'export CGO_CFLAGS = -O2 -g -std=gnu17 # keep\n'

	# F10 of the #9105 review: a define body has no CGO_CFLAGS= token but sets the
	# exported variable, so a later -std= in it wins.
	mk_case "with a define CGO_CFLAGS body FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\ndefine CGO_CFLAGS\n-O2 -std=gnu23\nendef\n'
	mk_case "with an override define CGO_CFLAGS FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\noverride define CGO_CFLAGS =\n-O2 -std=gnu23\nendef\n'
	mk_case "with a define ::= CGO_CFLAGS FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\ndefine CGO_CFLAGS ::=\n-O2\nendef\n'
	mk_case "with a target-specific CGO_CFLAGS adding -std= FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\nbuild: CGO_CFLAGS := -O2 -std=gnu23\n'
	mk_case "with \$(eval) setting CGO_CFLAGS FAILS" 1 'export CGO_CFLAGS := -O2 -g -std=gnu17\n$(eval CGO_CFLAGS := -O2 -std=gnu23)\n'
}
