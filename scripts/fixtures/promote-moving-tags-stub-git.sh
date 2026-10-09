#!/usr/bin/env bash
# Fake git for the promote-moving-tags hermetic suite. Answers
# merge-base --is-ancestor from ${STUB_DIR}/ancestors ("child parent" per
# line, transitive); fetch consumes ${STUB_DIR}/fetch-add into the ancestors
# file when a case provides it, modelling a clone refresh that resolves
# previously unknown objects; cat-file -e reports object existence. Exit
# codes match real git: merge-base 0/1/128, cat-file 0/128. Anything else is
# an error. Every call is logged.
set -euo pipefail
echo "git $*" >>"${STUB_DIR}/git.log"
if [[ "${1:-}" == "fetch" ]]; then
	if [[ -f "${STUB_DIR}/fetch-add" ]]; then
		cat "${STUB_DIR}/fetch-add" >>"${STUB_DIR}/ancestors"
		rm "${STUB_DIR}/fetch-add"
	fi
	exit 0
fi
known() {
	# known <object>: 0 when the object appears anywhere in the ancestors
	# file (the stub's whole object database).
	local obj="$1"
	[[ -n "${obj}" ]] || return 1
	[[ -f "${STUB_DIR}/ancestors" ]] || return 1
	grep -q -w -- "${obj}" "${STUB_DIR}/ancestors"
}
if [[ "${1:-}" == "cat-file" && "${2:-}" == "-e" ]]; then
	known "${3:-}" && exit 0 || exit 128
fi
if [[ "${1:-}" == "merge-base" && "${2:-}" == "--is-ancestor" ]]; then
	a="$3"
	b="$4"
	[[ "${a}" == "${b}" ]] && exit 0
	known "${a}" && known "${b}" || exit 128
	seen=" ${b} "
	queue=("${b}")
	while [[ "${#queue[@]}" -gt 0 ]]; do
		n="${queue[0]}"
		queue=("${queue[@]:1}")
		while read -r child parent _; do
			[[ "${child}" == "${n}" ]] || continue
			[[ "${parent}" == "${a}" ]] && exit 0
			if [[ "${seen}" != *" ${parent} "* ]]; then
				seen+=" ${parent} "
				queue+=("${parent}")
			fi
		done <"${STUB_DIR}/ancestors"
	done
	exit 1
fi
echo "git stub: unsupported: $*" >&2
exit 2
