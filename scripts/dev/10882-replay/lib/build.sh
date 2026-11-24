#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Builds every binary the replay runs from ONE commit and records the commit and
# the build identity of each (go version -m), so a control run and a treatment
# run can never mix builds. Product binaries: projector, ingester,
# collector-component-extension, collector-synth-zero, bootstrap-data-plane. The
# seeder is a build-tagged test program compiled with `go test -c` at the same
# commit (no new cmd directory).
# Needs env.sh. Usage: build.sh <repo-root> <results-root>
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$here/env.sh"

repo="${1:?usage: build.sh <repo-root> <results-root>}"
results="${2:?usage: build.sh <repo-root> <results-root>}"
mkdir -p "$REPLAY_BIN" "$results"
commit="$(git -C "$repo" rev-parse HEAD)"
if [[ -n "$(git -C "$repo" status --porcelain --untracked-files=no)" ]]; then
  echo "build.sh: tracked changes in $repo; build from a clean commit" >&2
  exit 1
fi
echo "$commit" >"$results/build.commit"
export GOTMPDIR="${GOTMPDIR:-$REPLAY_HOME/gotmp}" GOCACHE="${GOCACHE:-$REPLAY_HOME/gocache}"
mkdir -p "$GOTMPDIR"
(
  cd "$repo/go"
  for cmd in projector ingester collector-component-extension collector-synth-zero bootstrap-data-plane; do
    nice -n 5 go build -trimpath -o "$REPLAY_BIN/eshu-$cmd" "./cmd/$cmd"
  done
  nice -n 5 go test -c -tags replayseed -o "$REPLAY_BIN/replayseed.test" ./internal/storage/postgres
  nice -n 5 go test -c -tags replayseed -o "$REPLAY_BIN/replayseed-synth.test" ./cmd/collector-synth-zero
)
{
  echo "commit=$commit"
  for bin in "$REPLAY_BIN"/eshu-*; do
    echo "== $(basename "$bin") sha256=$(sha256sum "$bin" | cut -d' ' -f1)"
    go version -m "$bin" | rg "^\s+(path|mod|build\s+(vcs\.revision|vcs\.modified|-trimpath|CGO_ENABLED))" || true
  done
} >"$results/build.txt"
# Machine-readable build identity, checked by check_arm.py: every product binary
# must carry vcs.revision equal to the commit and vcs.modified=false.
python3 -I "$here/../build_identity.py" "$results/build.json" "$commit" "$REPLAY_BIN"
echo "built replay binaries at $commit"
