#!/usr/bin/env bash
#
# test-verify-apk-floors.sh - hermetic tests for scripts/verify-apk-floors.sh
# (#7571). No network and no Docker: the verifier is pointed at a file://
# mirror built under mktemp, holding real APKINDEX.tar.gz archives.
#
# The seeded RED/GREEN pair:
#   RED   scripts/fixtures/apk-floors/Dockerfile.r1-floors, the final stage as
#         origin/main had it before #7571 (libssl3/libcrypto3 floors at
#         3.3.7-r1), against a mirror serving 3.3.7-r2: the verifier must fail.
#   GREEN the repository Dockerfile against a mirror built FROM that
#         Dockerfile's own floors (served == floor): the verifier must pass.
#         Raising any served version by one revision must turn it RED again,
#         so the pass is not a verifier that ignores the index.
# Fail-closed cases: unreachable mirror, garbage archive, a floor package the
# index does not list, a non-Alpine final stage, and a Dockerfile with no
# floors must all exit 2, never 0.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-apk-floors.sh"
red_dockerfile="${repo_root}/scripts/fixtures/apk-floors/Dockerfile.r1-floors"
repo_dockerfile="${repo_root}/Dockerfile"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# make_mirror DIR BRANCH SPEC...: write <DIR>/<BRANCH>/<repo>/<arch>/APKINDEX.tar.gz
# for repos main and community and arches x86_64 and aarch64. Each SPEC is
# "pkg=version[@repo]"; the repo defaults to main.
make_mirror() {
	local dir="$1" branch="$2"
	shift 2
	local repo arch spec pkg ver where
	for repo in main community; do
		for arch in x86_64 aarch64; do
			mkdir -p "${dir}/${branch}/${repo}/${arch}"
			local stage="${dir}/stage.${repo}.${arch}"
			mkdir -p "${stage}"
			: >"${stage}/APKINDEX"
			# A decoy record so a parser that takes the first V: line fails.
			printf 'C:Q1decoy\nP:aaa-decoy\nV:9.9.9-r9\nA:%s\n\n' "${arch}" >>"${stage}/APKINDEX"
			for spec in "$@"; do
				pkg="${spec%%=*}"
				ver="${spec#*=}"
				where=main
				if [[ "${ver}" == *@* ]]; then where="${ver#*@}"; ver="${ver%%@*}"; fi
				[[ "${where}" == "${repo}" ]] || continue
				printf 'C:Q1x\nP:%s\nV:%s\nA:%s\n\n' "${pkg}" "${ver}" "${arch}" >>"${stage}/APKINDEX"
			done
			tar -czf "${dir}/${branch}/${repo}/${arch}/APKINDEX.tar.gz" -C "${stage}" APKINDEX
		done
	done
}

# run_verifier DOCKERFILE MIRROR_DIR -> sets rc and out_file.
run_verifier() {
	out_file="${tmp_root}/out.txt"
	set +e
	APK_FLOORS_MIRROR="file://$2" bash "${verifier}" --dockerfile "$1" >"${out_file}" 2>&1
	rc=$?
	set -e
}

expect_rc() {
	local want="$1" label="$2"
	if [[ "${rc}" -eq "${want}" ]]; then
		record_pass "${label} (rc=${rc})"
	else
		record_fail "${label}: want rc=${want}, got rc=${rc}"
		cat "${out_file}" >&2
	fi
}

expect_output() {
	local needle="$1" label="$2"
	if [[ "$(<"${out_file}")" == *"${needle}"* ]]; then
		record_pass "${label}"
	else
		record_fail "${label} (expected to find: ${needle})"
		cat "${out_file}" >&2
	fi
}

# floors_of DOCKERFILE -> "pkg=version" per floor, read with the same rule the
# verifier documents (name>=version tokens), so the GREEN mirror is derived
# from the Dockerfile and never hardcoded.
floors_of() {
	awk '
		/^RUN .*apk[ \t]+add/ { on = 1 }
		on { cont = ($0 ~ /\\$/); sub(/\\$/, ""); print; if (!cont) on = 0 }
	' "$1" | tr -d "\"'" | tr ' \t' '\n\n' | awk -F'>=' 'NF == 2 && $1 != "" { print $1 "=" $2 }'
}

# --- RED: the pre-#7571 floors against a repo serving r2. ------------------
mirror="${tmp_root}/red"
make_mirror "${mirror}" v3.21 "c-ares=1.34.8-r0" "libexpat=2.8.4-r0" \
	"libssl3=3.3.7-r2" "libcrypto3=3.3.7-r2"
run_verifier "${red_dockerfile}" "${mirror}"
expect_rc 1 "RED: r1 floors fail against a repository serving r2"
expect_output "FAIL x86_64 libssl3: floor 3.3.7-r1 is below the 3.3.7-r2" "RED names the stale package and both versions"
expect_output "FAIL aarch64 libcrypto3" "RED checks every arch"

# --- GREEN: the repository Dockerfile against its own floors. --------------
specs=()
while IFS= read -r spec; do specs+=("${spec}"); done < <(floors_of "${repo_dockerfile}")
[[ "${#specs[@]}" -gt 0 ]] || { echo "test-verify-apk-floors: no floors read from ${repo_dockerfile}" >&2; exit 1; }
mirror="${tmp_root}/green"
make_mirror "${mirror}" v3.21 "${specs[@]}"
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 0 "GREEN: the repository Dockerfile passes when served equals its floors"

# --- Sensitivity: bump each served version by one revision; every one must
# turn the verifier RED, so GREEN is not vacuous. A package listed in the
# community repo must be found there too.
for i in "${!specs[@]}"; do
	pkg="${specs[$i]%%=*}"
	ver="${specs[$i]#*=}"
	rev="${ver##*-r}"
	bumped=("${specs[@]}")
	bumped[i]="${pkg}=${ver%-r*}-r$((rev + 1))@community"
	mirror="${tmp_root}/bump-${i}"
	make_mirror "${mirror}" v3.21 "${bumped[@]}"
	run_verifier "${repo_dockerfile}" "${mirror}"
	expect_rc 1 "sensitivity: a newer served ${pkg} (community repo) turns the gate RED"
done

# A floor above the served version is a note, not a failure: the image build
# itself fails closed in that case.
mirror="${tmp_root}/ahead"
make_mirror "${mirror}" v3.21 "c-ares=1.34.7-r0" "libexpat=2.8.4-r0" \
	"libssl3=3.3.7-r2" "libcrypto3=3.3.7-r2"
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 0 "a floor above the served version does not fail the drift check"
expect_output "note x86_64 c-ares" "the ahead case is reported"

# Version ordering: 1.10 is newer than 1.9 (numeric, not lexical).
printf 'FROM alpine:3.21\nRUN apk add --no-cache "foo>=1.9-r0"\n' >"${tmp_root}/numeric.Dockerfile"
mirror="${tmp_root}/numeric"
make_mirror "${mirror}" v3.21 "foo=1.10-r0"
run_verifier "${tmp_root}/numeric.Dockerfile" "${mirror}"
expect_rc 1 "numeric component ordering: 1.10-r0 is newer than 1.9-r0"

# Revision ordering is numeric: r10 is newer than r2, not older.
printf 'FROM alpine:3.21\nRUN apk add --no-cache "foo>=1.0-r2"\n' >"${tmp_root}/rev.Dockerfile"
mirror="${tmp_root}/rev"
make_mirror "${mirror}" v3.21 "foo=1.0-r10"
run_verifier "${tmp_root}/rev.Dockerfile" "${mirror}"
expect_rc 1 "two-digit revision: served r10 is newer than floor r2"

# Several records of one package: the highest served version is what counts,
# whichever order the index lists them in.
mirror="${tmp_root}/multi-high-first"
make_mirror "${mirror}" v3.21 "foo=1.0-r10" "foo=1.0-r1"
run_verifier "${tmp_root}/rev.Dockerfile" "${mirror}"
expect_rc 1 "several records, highest listed first: the highest is compared"
mirror="${tmp_root}/multi-high-last"
make_mirror "${mirror}" v3.21 "foo=1.0-r1" "foo=1.0-r10"
run_verifier "${tmp_root}/rev.Dockerfile" "${mirror}"
expect_rc 1 "several records, highest listed last: the highest is compared"

# A comment line inside a RUN continuation does not end the logical line, so
# floors after it still count. Literal fixture, not floors_of: that helper
# shares this blind spot. The index lacks "bar": the verifier must notice.
comment_fixture="${repo_root}/scripts/fixtures/apk-floors/Dockerfile.comment-in-continuation"
mirror="${tmp_root}/comment-missing"
make_mirror "${mirror}" v3.21 "foo=3.3.7-r3"
run_verifier "${comment_fixture}" "${mirror}"
expect_rc 2 "a floor after a comment inside a continuation is still checked (missing package)"
expect_output "bar" "the floor after the comment is named"
mirror="${tmp_root}/comment-stale"
make_mirror "${mirror}" v3.21 "foo=3.3.7-r3" "bar=1.0-r1"
run_verifier "${comment_fixture}" "${mirror}"
expect_rc 1 "a stale floor after a comment inside a continuation turns the gate RED"
mirror="${tmp_root}/comment-ok"
make_mirror "${mirror}" v3.21 "foo=3.3.7-r3" "bar=1.0-r0"
run_verifier "${comment_fixture}" "${mirror}"
expect_rc 0 "both floors of the commented continuation pass when current"

# --- Fail closed. -----------------------------------------------------------
run_verifier "${repo_dockerfile}" "${tmp_root}/does-not-exist"
expect_rc 2 "fetch failure exits non-zero (never passes)"
expect_output "cannot fetch" "fetch failure names the URL it could not read"

mirror="${tmp_root}/garbage"
make_mirror "${mirror}" v3.21 "${specs[@]}"
printf 'not a tarball' >"${mirror}/v3.21/main/x86_64/APKINDEX.tar.gz"
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 2 "an unparseable archive exits non-zero"

mirror="${tmp_root}/empty-index"
make_mirror "${mirror}" v3.21 "${specs[@]}"
mkdir -p "${tmp_root}/blank"
: >"${tmp_root}/blank/APKINDEX"
tar -czf "${mirror}/v3.21/main/x86_64/APKINDEX.tar.gz" -C "${tmp_root}/blank" APKINDEX
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 2 "an index with no package records exits non-zero"

mirror="${tmp_root}/missing-pkg"
make_mirror "${mirror}" v3.21 "c-ares=1.34.8-r0" "libexpat=2.8.4-r0" "libssl3=3.3.7-r2"
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 2 "a floor package absent from the index exits non-zero"
expect_output "libcrypto3" "the missing package is named"

mirror="${tmp_root}/odd-version"
make_mirror "${mirror}" v3.21 "c-ares=1.34.8_p1-r0" "libexpat=2.8.4-r0" "libssl3=3.3.7-r2" "libcrypto3=3.3.7-r2"
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 2 "a served version of an unknown shape exits non-zero"

mirror="${tmp_root}/branch-only-310"
make_mirror "${mirror}" v3.20 "${specs[@]}"
run_verifier "${repo_dockerfile}" "${mirror}"
expect_rc 2 "the branch comes from the Dockerfile: a mirror lacking v3.21 fails"

printf 'FROM debian:12\nRUN apt-get install -y "libssl3>=3.0.0-r0"\n' >"${tmp_root}/debian.Dockerfile"
run_verifier "${tmp_root}/debian.Dockerfile" "${tmp_root}/green"
expect_rc 2 "a non-Alpine final stage exits non-zero"

printf 'FROM alpine:3.21\nRUN apk add --no-cache git curl\n' >"${tmp_root}/nofloors.Dockerfile"
run_verifier "${tmp_root}/nofloors.Dockerfile" "${tmp_root}/green"
expect_rc 2 "a final stage with no floors exits non-zero instead of passing vacuously"

# Only the FINAL stage counts: an earlier stage's floors are ignored.
printf 'FROM alpine:3.21 AS early\nRUN apk add --no-cache "zzz>=1.0-r0"\nFROM alpine:3.21\nRUN apk add --no-cache "foo>=1.0-r0"\n' >"${tmp_root}/stages.Dockerfile"
mirror="${tmp_root}/stages"
make_mirror "${mirror}" v3.21 "foo=1.0-r0"
run_verifier "${tmp_root}/stages.Dockerfile" "${mirror}"
expect_rc 0 "floors on earlier stages are ignored (zzz is absent and not checked)"

# The verifier must not edit the Dockerfile.
before="$(cksum <"${repo_dockerfile}")"
mirror="${tmp_root}/green"
run_verifier "${repo_dockerfile}" "${mirror}"
after="$(cksum <"${repo_dockerfile}")"
if [[ "${before}" == "${after}" ]]; then record_pass "the verifier leaves the Dockerfile unchanged"; else record_fail "the verifier modified the Dockerfile"; fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
