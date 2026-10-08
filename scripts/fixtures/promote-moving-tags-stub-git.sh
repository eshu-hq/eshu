#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "merge-base" && "${2:-}" == "--is-ancestor" ]]; then
	a="$3"
	b="$4"
	[[ "${a}" == "${b}" ]] && exit 0
	[ -f "${STUB_DIR}/ancestors" ] || exit 1
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
