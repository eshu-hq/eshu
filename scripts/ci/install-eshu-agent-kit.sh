#!/usr/bin/env bash
# Install the eshu-agent-kit this repo pins, so a gate that reads the shared
# agent material finds an installed tree.
#
# The shared skills, role bindings, and harness hooks live in the
# eshu-agent-kit, not in this repo. The installer materializes them locally
# as symlinks, and the gates read that installed tree. A CI job that runs a
# gate over that material must install the kit first.
#
# The kit carries no release tags yet, so the checkout is pinned to a full
# commit SHA: an enforcement gate must execute one immutable kit revision,
# and a floating HEAD could change under the gate. The archive's top-level
# directory name is discovered rather than computed, because GitHub names it
# from the ref (a SHA keeps the full hash, a tag drops the leading "v"), so
# a computed path would miss the checkout.
#
# Usage: scripts/ci/install-eshu-agent-kit.sh [target-repo]
# The target defaults to the current directory.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
target="${1:-$repo_root}"

# The pinned kit revision. Update it together with .agent-kit-version: the
# VERSION check below fails the job when the two disagree, so a forgotten
# bump is loud rather than silent.
readonly ESHU_AGENT_KIT_REF="${ESHU_AGENT_KIT_REF:-a0c64a7cfe3c53c3e6629d53d9e32e5771ea5cdd}"

# The kit is extracted somewhere DURABLE. The installer links the shared
# material into the repo as symlinks, and a symlink into a deleted temp
# directory leaves the repo pointing at nothing.
kit_dir="${target}/eshu-agent-kit"
mkdir -p "${kit_dir}"
archive="$(mktemp)"
trap 'rm -f "${archive}"' EXIT

curl -fsSL "https://codeload.github.com/eshu-hq/eshu-agent-kit/tar.gz/${ESHU_AGENT_KIT_REF}" \
  -o "${archive}"
tar -xzf "${archive}" -C "${kit_dir}" --strip-components=1
kit="${kit_dir}"
if [ ! -x "${kit}/install.sh" ]; then
  echo "install-eshu-agent-kit: the kit archive did not contain install.sh" >&2
  exit 1
fi

# The pin and the checked-out revision must agree, so the VERSION the kit
# carries and the version this repo pins cannot drift apart silently.
pinned="$(tr -d '[:space:]' < "${repo_root}/.agent-kit-version")"
carried="$(tr -d '[:space:]' < "${kit}/VERSION")"
if [ "${pinned}" != "${carried}" ]; then
  echo "install-eshu-agent-kit: version mismatch: repo pins ${pinned}, checkout carries ${carried}" >&2
  echo "fix: update .agent-kit-version, or pin ESHU_AGENT_KIT_REF to a revision carrying ${pinned}" >&2
  exit 1
fi

# Every mode the gates read: --repo materializes the skills and their alias
# wrappers, --agents the role manifest, the role shims and the launcher,
# --hooks the hook scripts, --rules the path-scoped rules.
"${kit}/install.sh" --repo "${target}" --aliases "${kit}/templates/repo-overlay/eshu-aliases.tsv"
"${kit}/install.sh" --agents --repo "${target}"
"${kit}/install.sh" --hooks --repo "${target}"
"${kit}/install.sh" --rules --repo "${target}"

echo "install-eshu-agent-kit: installed kit ${ESHU_AGENT_KIT_REF} into ${target}"
