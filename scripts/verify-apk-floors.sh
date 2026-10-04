#!/usr/bin/env bash
# verify-apk-floors.sh - fails when an `apk add "pkg>=version"` floor in the
# Dockerfile's final stage is older than the version the Alpine repository
# currently serves (#7571).
#
# Why: the published image once shipped libssl3/libcrypto3 3.3.7-r1 although
# the repository already served 3.3.7-r2, because a reused layer-cache entry
# was never re-resolved. The floors are the fail-closed guard; this script is
# the drift detector that says a floor needs raising.
#
# Everything is derived from the Dockerfile, nothing is hardcoded:
#   - the Alpine branch comes from the final stage's `FROM alpine:X.Y` line;
#   - the package list and floors come from the `apk add` RUN lines of that
#     stage (a floor is a `name>=version` token).
# The repository state comes from the index the pinned base image itself
# reads: <mirror>/vX.Y/<repo>/<arch>/APKINDEX.tar.gz, for every arch in
# APK_FLOORS_ARCHES and every repo in APK_FLOORS_REPOS.
#
# It never edits the Dockerfile and never pins an exact version. Integrity of
# the index relies on HTTPS to the mirror; the .SIGN signature is not checked.
#
# Usage:
#   scripts/verify-apk-floors.sh [--dockerfile PATH]
#
# Environment:
#   APK_FLOORS_MIRROR  Alpine mirror root (default https://dl-cdn.alpinelinux.org/alpine).
#                      A file:// URL serves the self-test fixtures.
#   APK_FLOORS_ARCHES  Space-separated arches (default "x86_64 aarch64", the
#                      platforms docker-publish.yml builds).
#   APK_FLOORS_REPOS   Space-separated repos (default "main community").
#
# Exit codes:
#   0 - every floor is at or above what the repository serves.
#   1 - at least one floor is below what the repository serves (stale).
#   2 - fail closed: bad usage, unreadable or unparseable Dockerfile or index,
#       fetch failure, or a floor package the repository does not list. A
#       network error never passes and never skips.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dockerfile="${repo_root}/Dockerfile"
mirror="${APK_FLOORS_MIRROR:-https://dl-cdn.alpinelinux.org/alpine}"
arches="${APK_FLOORS_ARCHES:-x86_64 aarch64}"
repos="${APK_FLOORS_REPOS:-main community}"

die() {
	printf 'verify-apk-floors: FAIL-CLOSED: %s\n' "$*" >&2
	exit 2
}

while (($# > 0)); do
	case "$1" in
		--dockerfile)
			(($# >= 2)) || die "--dockerfile needs a path"
			dockerfile="$2"
			shift 2
			;;
		*)
			printf 'verify-apk-floors: unknown argument %s\n' "$1" >&2
			exit 2
			;;
	esac
done

for tool in awk curl tar; do
	command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done
[[ -r "${dockerfile}" ]] || die "cannot read Dockerfile: ${dockerfile}"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# Final stage: the last FROM line and the logical lines (continuations joined)
# after it. Prints "IMAGE <ref>" then one "RUN <text>" per RUN line.
awk '
	function flush() { if (line != "") { emit(line); line = "" } }
	function emit(l,   u, n, f, i, img) {
		u = l; sub(/^[ \t]+/, "", u)
		if (toupper(substr(u, 1, 5)) == "FROM " || toupper(substr(u, 1, 5)) == "FROM\t") {
			n = split(u, f, /[ \t]+/); img = ""
			for (i = 2; i <= n; i++) { if (f[i] !~ /^--/) { img = f[i]; break } }
			image = img; nruns = 0
		} else if (toupper(substr(u, 1, 4)) == "RUN " || toupper(substr(u, 1, 4)) == "RUN\t") {
			runs[++nruns] = u
		}
	}
	/^[ \t]*#/ { next }
	{
		s = $0
		if (s ~ /\\[ \t]*$/) { sub(/\\[ \t]*$/, "", s); line = line s " "; next }
		line = line s; flush()
	}
	END {
		flush()
		if (image == "") exit 3
		print "IMAGE " image
		for (i = 1; i <= nruns; i++) print "RUN " runs[i]
	}
' "${dockerfile}" >"${work}/final-stage.txt" || die "no FROM line found in ${dockerfile}"

image="$(awk '$1 == "IMAGE" { print $2; exit }' "${work}/final-stage.txt")"
if [[ ! "${image}" =~ ^alpine:([0-9]+\.[0-9]+)([@:].*)?$ ]]; then
	die "final stage base image '${image}' is not alpine:<major>.<minor>; cannot derive the repository branch"
fi
branch="v${BASH_REMATCH[1]}"

# Floors: name>=version tokens on the final stage's `apk add` RUN lines.
floor_re='^([A-Za-z0-9][A-Za-z0-9._+-]*)>=([0-9][A-Za-z0-9._-]*)$'
: >"${work}/floors.txt"
set -f
while IFS= read -r run_line; do
	for token in ${run_line}; do
		token="${token//\"/}"
		token="${token//\'/}"
		if [[ "${token}" =~ ${floor_re} ]]; then
			printf '%s %s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" >>"${work}/floors.txt"
		fi
	done
done < <(awk '/^RUN / && /apk[ \t]+add/' "${work}/final-stage.txt")
set +f
[[ -s "${work}/floors.txt" ]] ||
	die "no 'name>=version' floors found on an apk add line of the final stage; the guard would be vacuous"

# version_parts VERSION -> "<dotted> <rev>"; empty when the shape is not
# N(.N)*[-rN]. A missing revision counts as r0.
version_parts() {
	if [[ "$1" =~ ^([0-9]+(\.[0-9]+)*)(-r([0-9]+))?$ ]]; then
		printf '%s %s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[4]:-0}"
	fi
}

# version_cmp A B -> prints -1, 0 or 1. Numeric component-wise (missing
# components are zero), then the -rN revision. Callers validate shape first.
version_cmp() {
	local pa pb a b ra rb i x y n
	pa="$(version_parts "$1")"
	pb="$(version_parts "$2")"
	IFS=' ' read -r a ra <<<"${pa}"
	IFS=' ' read -r b rb <<<"${pb}"
	local -a av bv
	IFS='.' read -r -a av <<<"${a}"
	IFS='.' read -r -a bv <<<"${b}"
	n="${#av[@]}"
	((${#bv[@]} > n)) && n="${#bv[@]}"
	for ((i = 0; i < n; i++)); do
		x=$((10#${av[i]:-0}))
		y=$((10#${bv[i]:-0}))
		if ((x < y)); then printf -- '-1'; return; fi
		if ((x > y)); then printf '1'; return; fi
	done
	x=$((10#${ra}))
	y=$((10#${rb}))
	if ((x < y)); then printf -- '-1'; elif ((x > y)); then printf '1'; else printf '0'; fi
}

# served_versions INDEX PACKAGE -> every version listed for PACKAGE.
served_versions() {
	awk -v pkg="$2" '
		BEGIN { RS = ""; FS = "\n" }
		{
			p = ""; v = ""
			for (i = 1; i <= NF; i++) {
				if ($i ~ /^P:/) p = substr($i, 3)
				if ($i ~ /^V:/) v = substr($i, 3)
			}
			if (p == pkg) print v
		}
	' "$1"
}

stale=0
checked=0
for arch in ${arches}; do
	: >"${work}/served.${arch}"
	for repo in ${repos}; do
		url="${mirror}/${branch}/${repo}/${arch}/APKINDEX.tar.gz"
		archive="${work}/${repo}.${arch}.tar.gz"
		if ! curl -fsS --retry 3 --retry-delay 2 --max-time 120 -o "${archive}" "${url}" 2>"${work}/curl.err"; then
			die "cannot fetch ${url}: $(tr '\n' ' ' <"${work}/curl.err")"
		fi
		index="${work}/${repo}.${arch}.APKINDEX"
		if ! tar -xzOf "${archive}" APKINDEX >"${index}" 2>"${work}/tar.err"; then
			die "cannot unpack the APKINDEX from ${url}: $(tr '\n' ' ' <"${work}/tar.err")"
		fi
		awk 'BEGIN { rc = 1 } /^P:/ { rc = 0; exit } END { exit rc }' "${index}" || die "${url} holds no package records; the index is unparseable"
		cat "${index}" >>"${work}/served.${arch}"
		printf '\n' >>"${work}/served.${arch}"
	done

	while read -r pkg floor; do
		[[ -n "$(version_parts "${floor}")" ]] ||
			die "floor '${pkg}>=${floor}' is not of the form N(.N)*[-rN]"
		versions="$(served_versions "${work}/served.${arch}" "${pkg}")"
		[[ -n "${versions}" ]] ||
			die "package '${pkg}' is not listed in ${branch} (${repos}) for ${arch}"
		while read -r served; do
			[[ -n "$(version_parts "${served}")" ]] ||
				die "${branch}/${arch} serves '${pkg}' at '${served}', which is not of the form N(.N)*-rN"
			checked=$((checked + 1))
			case "$(version_cmp "${floor}" "${served}")" in
				-1)
					stale=$((stale + 1))
					printf 'FAIL %s %s: floor %s is below the %s served by the repository\n' \
						"${arch}" "${pkg}" "${floor}" "${served}" >&2
					;;
				0) printf 'ok   %s %s: floor %s equals the served version\n' "${arch}" "${pkg}" "${floor}" ;;
				1) printf 'note %s %s: floor %s is above the served %s; the image build itself fails closed\n' \
					"${arch}" "${pkg}" "${floor}" "${served}" ;;
			esac
		done <<<"${versions}"
	done <"${work}/floors.txt"
done

((checked > 0)) || die "no floor was compared; refusing to pass"

if ((stale > 0)); then
	printf 'verify-apk-floors: %d floor(s) in %s are stale for %s; raise them to the served version\n' \
		"${stale}" "${dockerfile}" "${branch}" >&2
	exit 1
fi
printf 'verify-apk-floors: PASS (%d comparisons against %s)\n' "${checked}" "${branch}"
