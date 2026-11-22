#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Builds the local git fixture repositories the real ingester collects during
# the replay (arbiter item 6). Content, author, dates and messages are fixed, so
# every arm collects byte-identical commits and the fact set (fact id and payload
# hash) of each repository can be compared across arms.
#
# Usage: make_fixtures.sh <root> [count]
set -euo pipefail

root="${1:?usage: make_fixtures.sh <root> [count]}"
count="${2:-6}"
rm -rf "$root"
mkdir -p "$root"

export GIT_AUTHOR_NAME="replay" GIT_AUTHOR_EMAIL="replay@example.invalid"
export GIT_COMMITTER_NAME="replay" GIT_COMMITTER_EMAIL="replay@example.invalid"
export GIT_AUTHOR_DATE="2026-01-01T00:00:00Z" GIT_COMMITTER_DATE="2026-01-01T00:00:00Z"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null

for n in $(seq 1 "$count"); do
  i="$(printf "%02d" "$n")"
  repo="$root/replay-fixture-$i"
  mkdir -p "$repo/src"
  git -C "$repo" init -q -b main
  for f in $(seq 1 8); do
    cat >"$repo/src/mod$f.go" <<GO
package fixture$i

// Add$f returns the sum of its arguments plus $f.
func Add$f(a, b int) int { return a + b + $f }

// Name$f identifies module $f of fixture $i.
func Name$f() string { return "fixture-$i-module-$f" }
GO
  done
  printf '# replay fixture %s\n' "$i" >"$repo/README.md"
  git -C "$repo" add -A
  git -C "$repo" commit -q -m "replay fixture $i"
done
echo "built $count fixture repositories under $root"
