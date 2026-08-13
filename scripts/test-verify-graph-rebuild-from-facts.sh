#!/usr/bin/env bash
# Test mirror and BITES proof for the identity-snapshot safety check in
# scripts/verify-graph-rebuild-from-facts.sh (#4594).
#
# That gate compares two graphs by their node and edge identities. The check
# under test is what stops it comparing identities that cannot tell anything
# apart -- a file of interchangeable keys diffs clean against any other such
# file, so the gate would report a match while proving nothing.
#
# This sources the REAL function out of the gate script (ESHU_DR_SOURCE_ONLY
# stops the procedure itself from running), so every assertion here exercises
# the shipped code rather than a copy of it. No Docker, no Compose, no network.
#
# Usage: scripts/test-verify-graph-rebuild-from-facts.sh
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=verify-graph-rebuild-from-facts.sh
ESHU_DR_SOURCE_ONLY=true source "${script_root}/verify-graph-rebuild-from-facts.sh"

# The gate sets -e for its own run; sourcing it applies that here too, and the
# whole point of this file is to call a function that returns non-zero on
# purpose. Turn it back off after the source, not before.
set +e

pass_count=0
fail_count=0

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
	return 0
}

# new_snapshot writes a nodes.txt and an edges.txt into a fresh directory and
# prints its path. Arguments are the two file bodies, in that order.
new_snapshot() {
	local dir
	dir="$(mktemp -d)"
	printf '%s' "$1" >"${dir}/nodes.txt"
	printf '%s' "$2" >"${dir}/edges.txt"
	printf '%s\n' "${dir}"
}

# A healthy snapshot: every key carries content that distinguishes it. The
# trailing separators are normal -- most labels populate two or three of the
# concatenated properties.
healthy_nodes='File|src/main.go||src/main.go|repo-1|
Module|||||go
Repository|repo-1|eshu|||
'
healthy_edges='File|src/main.go||src/main.go|repo-1|||IMPORTS||Module|||||go
Repository|repo-1|eshu||||CONTAINS||File|src/main.go||src/main.go|repo-1|
'

# assert_refuses runs the check and expects a refusal.
assert_refuses() {
	local name="$1" dir="$2" output status
	output="$(assert_identity_snapshot_sane "${dir}" 2>&1)"
	status=$?
	rm -rf "${dir}"
	if [[ "${status}" -eq 0 ]]; then
		record_fail "${name}" "the check returned 0; this snapshot compares equal to anything"
		return 0
	fi
	record_pass "${name}"
}

# assert_accepts runs the check and expects it to pass.
assert_accepts() {
	local name="$1" dir="$2" output status
	output="$(assert_identity_snapshot_sane "${dir}" 2>&1)"
	status=$?
	rm -rf "${dir}"
	if [[ "${status}" -ne 0 ]]; then
		record_fail "${name}" "the check refused a legitimate snapshot: ${output}"
		return 0
	fi
	record_pass "${name}"
}

assert_accepts "a snapshot whose keys carry content is compared" \
	"$(new_snapshot "${healthy_nodes}" "${healthy_edges}")"

# The identity is a concatenation of coalesce()d properties. A node carrying
# none of them yields nothing but separators, and the label prefix makes the
# line non-empty, so an emptiness check passes it while every such node compares
# equal to every other.
assert_refuses "node keys that are nothing but separators are refused" \
	"$(new_snapshot 'Module|||||
File|src/main.go||src/main.go|repo-1|
' "${healthy_edges}")"

# The same collapse with the whole concatenation returning null: jq renders it
# as an empty line, and the label prefix leaves `Module|`.
assert_refuses "a node key that collapsed to the label prefix is refused" \
	"$(new_snapshot 'Module|
File|src/main.go||src/main.go|repo-1|
' "${healthy_edges}")"

# An edge endpoint that collapsed the same way leaves six separators against the
# `||` type delimiter -- at the head for the source, at the tail for the target.
assert_refuses "an edge whose source identity collapsed is refused" \
	"$(new_snapshot "${healthy_nodes}" '||||||IMPORTS||Module|||||go
')"
assert_refuses "an edge whose target identity collapsed is refused" \
	"$(new_snapshot "${healthy_nodes}" 'File|src/main.go||src/main.go|repo-1|||IMPORTS||||||
')"

assert_refuses "an empty node snapshot is refused" \
	"$(new_snapshot '' "${healthy_edges}")"

# An empty edge file diffs clean against any other empty edge file, and this
# backend has been seen returning zero rows for a query shape it dislikes
# without raising an error.
assert_refuses "an empty edge snapshot is refused" \
	"$(new_snapshot "${healthy_nodes}" '')"

# The documented false positive: `null` unanchored also matches real Terraform
# resource names, which would fail the gate on good data.
assert_accepts "a key containing null_resource is not mistaken for a null key" \
	"$(new_snapshot 'TerraformResource|null_resource.network_placeholder|network_placeholder|main.tf|repo-1|
File|src/main.go||src/main.go|repo-1|
' 'TerraformResource|null_resource.network_placeholder|network_placeholder|main.tf|repo-1|||DECLARED_IN||File|src/main.go||src/main.go|repo-1|
')"

printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
[[ "${fail_count}" -eq 0 ]]
