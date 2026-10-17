#!/usr/bin/env bash
# Scoped go vet for the merge_group slim lane (#7859 P3): vets the Go packages
# changed between two refs plus their direct importers, then stops. The full
# module vet is the pull-request lane's job; the queue lane only needs the
# interaction proof a whole-module build cannot give (a test-only break such
# as #7847's `undefined: catalogTestScope` compiles fine under
# `go build ./...`).
#
# Importer detection uses the `go list` import graph (Imports, TestImports and
# XTestImports), never string search over import paths: string search misses
# importers that only reach a changed package through a test file or a build
# tag, and it false-positives on comments and strings. The worst-case fan-out
# (core packages such as facts or reducer) is bounded by vetting direct
# importers only, never the transitive closure.
#
# Usage:
#   scripts/verify-merge-group-slim-vet.sh [--base REF] [--head REF]
#
# Defaults: --head HEAD, --base HEAD~1 (the merge_group layout: one group
# commit on its fixed base). Override both for any other shape. Either ref
# failing to resolve is a hard error: the gate must never pass blind.
#
# Exit 0 when no Go package changed. Changed files that map to no package
# (go.mod/go.sum, deleted packages, testdata) are skipped, never vetted
# under a synthetic import path. Any `go list` or `go vet` failure exits
# non-zero.
set -euo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_root}/.." && pwd)"
go_dir="${repo_root}/go"

base="HEAD~1"
head="HEAD"
while [[ $# -gt 0 ]]; do
	case "$1" in
		--base)
			[[ $# -ge 2 ]] || { printf 'verify-merge-group-slim-vet: --base needs a value\n' >&2; exit 2; }
			base="$2"
			shift 2
			;;
		--head)
			[[ $# -ge 2 ]] || { printf 'verify-merge-group-slim-vet: --head needs a value\n' >&2; exit 2; }
			head="$2"
			shift 2
			;;
		--help | -h)
			printf 'usage: verify-merge-group-slim-vet.sh [--base REF] [--head REF]\n'
			exit 0
			;;
		*)
			printf 'verify-merge-group-slim-vet: unknown argument %q\n' "$1" >&2
			exit 2
			;;
	esac
done

base_sha="$(git -C "${repo_root}" rev-parse --verify "${base}^{commit}")" || {
	printf 'verify-merge-group-slim-vet: cannot resolve --base %q\n' "${base}" >&2
	exit 1
}
head_sha="$(git -C "${repo_root}" rev-parse --verify "${head}^{commit}")" || {
	printf 'verify-merge-group-slim-vet: cannot resolve --head %q\n' "${head}" >&2
	exit 1
}

module="$(sed -n 's/^module[[:space:]]*//p' "${go_dir}/go.mod" | head -n 1)"
[[ -n "${module}" ]] || {
	printf 'verify-merge-group-slim-vet: cannot read module path from go/go.mod\n' >&2
	exit 1
}

changed_list="$(mktemp)"
trap 'rm -f "${changed_list}" "${graph_file:-}"' EXIT
git -C "${repo_root}" diff --name-only -z "${base_sha}" "${head_sha}" -- go/ 2>/dev/null | tr '\0' '\n' | grep -v '^$' >"${changed_list}" || true
if [[ ! -s "${changed_list}" ]]; then
	printf 'verify-merge-group-slim-vet: no files changed under go/ between %s and %s; nothing to vet\n' "${base}" "${head}"
	exit 0
fi

graph="$(cd "${go_dir}" && go list -e -f '{{.ImportPath}}|{{.Dir}}|{{join .Imports " "}}|{{join .TestImports " "}}|{{join .XTestImports " "}}' ./...)" || {
	printf 'verify-merge-group-slim-vet: go list failed; failing closed\n' >&2
	exit 1
}
[[ -n "${graph}" ]] || {
	printf 'verify-merge-group-slim-vet: go list printed no packages; failing closed\n' >&2
	exit 1
}

graph_file="$(mktemp)"
trap 'rm -f "${changed_list}" "${graph_file}"' EXIT
printf '%s\n' "${graph}" >"${graph_file}"

targets="$(awk -v godir="${go_dir}" '
	function pkgdir(dir,   d, prev) {
		d = dir
		while (d != "" && d != ".") {
			if (d in dir2pkg) return d
			prev = d
			sub(/\/[^\/]*$/, "", d)
			if (d == prev) break
		}
		return ""
	}
	FNR == NR {
		n = split($0, f, "|")
		importpath = f[1]
		dir = f[2]
		reldir = dir
		sub("^" godir "/", "", reldir)
		dir2pkg[reldir] = importpath
		imp = ""
		for (i = 3; i <= n; i++) imp = imp " " f[i]
		pkgimports[importpath] = imp
		allpkgs[importpath] = 1
		next
	}
	{
		file = $0
		sub(/^go\//, "", file)
		d = file
		sub(/\/[^\/]*$/, "", d)
		pd = pkgdir(d)
		if (pd != "") {
			changed[dir2pkg[pd]] = 1
		} else {
			# No package owns this directory (a top-level file such as
			# go.mod, a deleted package, or testdata): never synthesize
			# a would-be import path for it — vetting a path no package
			# owns either fails the gate or vets nothing real. Such
			# files route to the documented exit-0 path when nothing
			# else changed.
			printf "skipped\t%s\n", file
		}
	}
	END {
		for (p in changed) printf "changed\t%s\n", p
		for (p in allpkgs) {
			nimports = split(pkgimports[p], deps, " ")
			for (i = 1; i <= nimports; i++) {
				if (deps[i] in changed) {
					printf "importer\t%s\n", p
					break
				}
			}
		}
	}
' "${graph_file}" "${changed_list}")"
vet_list="$(printf '%s\n' "${targets}" | awk -F'\t' '$1 == "changed" || $1 == "importer" {print $2}' | sort -u)"
skipped_list="$(printf '%s\n' "${targets}" | awk -F'\t' '$1 == "skipped" {print $2}' | sort -u)"
if [[ -z "${vet_list}" ]]; then
	printf 'verify-merge-group-slim-vet: no Go package changed between %s and %s; nothing to vet\n' "${base}" "${head}"
	if [[ -n "${skipped_list}" ]]; then
		printf 'verify-merge-group-slim-vet: skipped %s non-package file(s):\n' "$(printf '%s\n' "${skipped_list}" | wc -l | tr -d ' ')"
		printf '%s\n' "${skipped_list}" | sed 's/^/  /'
	fi
	exit 0
fi
count="$(printf '%s\n' "${vet_list}" | wc -l | tr -d ' ')"
printf 'verify-merge-group-slim-vet: vetting %s package(s) (changed plus direct importers)\n' "${count}"
printf '%s\n' "${vet_list}" | sed 's/^/  /'

# Word-splitting vet_list is deliberate: Go import paths contain no spaces.
# shellcheck disable=SC2086
(cd "${go_dir}" && go vet ${vet_list})
