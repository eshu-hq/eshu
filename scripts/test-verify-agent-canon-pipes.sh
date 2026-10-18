#!/usr/bin/env bash
#
# test-verify-agent-canon-pipes.sh — static self-guard for the agent-canon
# scripts' assertion idiom (#8278).
#
# Feeding a variable to quiet ripgrep through a pipe is racy under
# `set -o pipefail`: the quiet reader exits on first match without draining
# its stdin, the writer dies with SIGPIPE (141), and pipefail promotes the
# pipeline to failure, so every match reads as a miss. The canon suites run
# under pipefail (companions inherit it from their sourcing runner), so the
# piped form is banned in the files this guard scans. The herestring form is
# the replacement (proven 500/500 clean).
#
# Forms this guard must NOT flag (all drain stdin or use no pipe): quiet
# matches that read a FILE argument, and piped readers without quiet such as
# the -o, -n, and -c readers, or python3 and head.
#
# The banned line shapes are built from fragments below, never spelled out:
# the guard scans itself, and a literal banned line in this file (even in a
# comment or a seeded fixture) would trip it. Same precedent as the
# private-identifier lib cases, which build their tokens from fragments.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/.." && pwd)"

# Every script the agent-canon gate executes: the verifier
# (verify-agent-canon.sh), its test mirror (plus the two lib case files the
# mirror sources), the hook suites, and every companion they source. This
# guard itself is last: it must pass its own scan. The registry-drift check
# below pins this list to the gate's trigger paths, so a future canon script
# cannot slip past unscanned.
canon_scripts=(
  "scripts/verify-agent-canon.sh"
  "scripts/test-verify-agent-hygiene.sh"
  "scripts/lib/test-private-identifier-cases.sh"
  "scripts/lib/test-private-identifier-path-cases.sh"
  "scripts/test-agent-hooks.sh"
  "scripts/test-goal-continue-hook.sh"
  "scripts/test-goal-continue-hook-cases.sh"
  "scripts/test-goal-continue-hook-budget-cases.sh"
  "scripts/test-goal-refresh-hook.sh"
  "scripts/test-goal-refresh-hook-cases.sh"
  "scripts/test-goal-refresh-hook-guard-cases.sh"
  "scripts/test-goal-refresh-hook-parity-cases.sh"
  "scripts/test-goal-refresh-hook-hermetic-cases.sh"
  "scripts/test-goal-refresh-hook-atomic-consent-cases.sh"
  "scripts/test-goal-refresh-hook-oneline-consent-cases.sh"
  "scripts/test-goal-refresh-hook-skill-nudge-cases.sh"
  "scripts/test-muse-hooks.sh"
  "scripts/test-cursor-hooks.sh"
  "scripts/test-cursor-hooks-helper-cases.sh"
  "scripts/test-verify-agent-canon-pipes.sh"
)

# Agent-canon trigger-path shell scripts this scan deliberately does NOT
# cover, with why. verify-no-ai-attribution.sh shares the hygiene workflow
# but belongs to the no-ai-attribution gate: it asserts attribution text, not
# hook behavior, so the pipe-idiom scan is not its owner. A new trigger-path
# script belongs in NEITHER list until someone classifies it here, and the
# drift check below fails until they do.
canon_trigger_exceptions=(
  "scripts/verify-no-ai-attribution.sh"
)

# Prints every scripts/*.sh trigger path of the agent-canon gate entry in the
# given registry file, one per line, with registry globs expanded against the
# tree. Reads the registry by file argument and matches trigger lines with a
# shell pattern, never with a piped reader, so this function itself carries
# none of the banned shapes it protects.
registry_trigger_scripts() { # registry-file
  local file="$1" in_entry=0 line pat e
  while IFS= read -r line; do
    case "$line" in
      *"- id: agent-canon"*) in_entry=1 ;;
      "  - id: "*)
        if [ "$in_entry" -eq 1 ]; then break; fi ;;
    esac
    if [ "$in_entry" -eq 0 ]; then continue; fi
    # Trigger lines are double-quoted today; the match also accepts single
    # quotes, a trailing comment, and bare unquoted tokens, so a future
    # style change lands in the seed/real checks (as an unclassified entry)
    # rather than silently skipping the line.
    pat=""
    if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*[\'\"]([^\"\']+)[\'\"]([[:space:]]*(#.*)?)?$ ]]; then
      pat="${BASH_REMATCH[1]}"
    elif [[ "$line" =~ ^[[:space:]]*-[[:space:]]*(scripts/[^[:space:]#]+)[[:space:]]*(#.*)?$ ]]; then
      pat="${BASH_REMATCH[1]}"
    fi
    if [[ -n "$pat" ]]; then
      case "$pat" in
        scripts/*.sh)
          if [[ "$pat" == *[*?\[]* ]]; then
            while IFS= read -r e; do
              printf '%s\n' "${e#"$repo_root"/}"
            done < <(compgen -G "$repo_root/$pat" || true)
          else
            printf '%s\n' "$pat"
          fi
          ;;
      esac
    fi
  done < "$file"
}

# Prints the registry trigger-path shell scripts that are neither scanned in
# canon_scripts nor classified in canon_trigger_exceptions. Empty output means
# the scan covers the whole trigger family.
unclassified_trigger_scripts() { # registry-file
  local file="$1" rel candidate found seen_dupe
  registry_trigger_scripts "$file" >"$tmp/triggers.txt"
  local -a seen=()
  while IFS= read -r rel; do
    if [ -z "$rel" ]; then continue; fi
    seen_dupe=0
    for candidate in "${seen[@]:-}"; do
      if [ "$candidate" = "$rel" ]; then seen_dupe=1; break; fi
    done
    if [ "$seen_dupe" -eq 1 ]; then continue; fi
    seen+=("$rel")
    found=0
    for candidate in "${canon_scripts[@]}" "${canon_trigger_exceptions[@]}"; do
      if [ "$candidate" = "$rel" ]; then found=1; break; fi
    done
    if [ "$found" -eq 0 ]; then printf '%s\n' "$rel"; fi
  done < "$tmp/triggers.txt"
}

# Fragment-built detector. stage1 finds a writer piped into the reader;
# stage2 keeps only the quiet invocations (short-flag bundle holding q, or
# the long quiet flag). Written this way the assembled pattern never appears
# as literal text in this file, so the self-scan below stays clean.
frag_writer='printf'
frag_reader='rg'
ws='[[:space:]]'
stage1_re="${frag_writer}[^|]*\\|${ws}*${frag_reader}${ws}"
quiet_re="[${ws}:]-[A-Za-z]*q[A-Za-z]*(${ws}|\$)|[${ws}:]--quiet(${ws}|\$)"

scan_hits() { # paths... -> prints file:line:content for banned lines
  local stage1_out
  stage1_out="$(rg -n --no-heading -e "$stage1_re" -- "$@" || true)"
  if [ -z "$stage1_out" ]; then
    return 0
  fi
  rg -e "$quiet_re" <<<"$stage1_out" || true
}

pass=0
fail=0
ok() { printf 'ok - %s\n' "$1"; pass=$((pass + 1)); }
no() { printf 'NOT OK - %s\n' "$1"; fail=$((fail + 1)); }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# A listed script that no longer exists is drift, not a pass.
missing=0
for rel in "${canon_scripts[@]}" "${canon_trigger_exceptions[@]}"; do
  if [ ! -f "$repo_root/$rel" ]; then
    no "canon scan target missing: $rel"
    missing=1
  fi
done
if [ "$missing" -eq 0 ]; then
  ok "all canon scan targets exist"
fi

# Seeded RED: a registry fixture naming a trigger-path script that is neither
# scanned nor classified must come back unclassified, by name. If this case
# ever stops flagging the fixture, the drift guard below is vacuous.
cat >"$tmp/seed-registry.yaml" <<'EOF'
  - id: agent-canon
    triggers:
      - "scripts/test-verify-agent-hygiene.sh"
      - "scripts/seeded-unlisted.sh"
      - 'scripts/seeded-single-quoted.sh'  # non-double-quote style
      - scripts/seeded-bare.sh  # unquoted style
  - id: next-gate
    triggers:
      - "scripts/seeded-other-gate.sh"
EOF
seed_unclassified="$(unclassified_trigger_scripts "$tmp/seed-registry.yaml")"
seed_expected="$(printf 'scripts/seeded-unlisted.sh\nscripts/seeded-single-quoted.sh\nscripts/seeded-bare.sh')"
if [ "$seed_unclassified" = "$seed_expected" ]; then
  ok "drift guard flags an unlisted trigger-path script by name (any style)"
else
  no "drift guard missed a planted unlisted trigger script -- coverage can silently rot"
fi

# The real drift check: every scripts/*.sh trigger of the agent-canon gate
# entry is either scanned above or classified in canon_trigger_exceptions.
# A future canon script added to the registry without classification fails
# here until someone lists it, so coverage cannot silently rot.
real_unclassified="$(unclassified_trigger_scripts "$repo_root/specs/ci-gates.d/gates-02.yaml")"
if [ -z "$real_unclassified" ]; then
  ok "every agent-canon trigger-path script is scanned or classified"
else
  no "agent-canon trigger-path scripts escape the scan:"
  printf '%s\n' "$real_unclassified" >&2
fi

# Seeded RED: the detector must fire on the banned shapes, in both the short
# and long quiet spellings. Built from fragments so this file never carries
# a literal banned line. A detector that cannot see a planted violation is
# vacuous, so this case failing means the guard below proves nothing.
bar='|'
printf '%s\n' \
  "${frag_writer} '%s' \"\$V\" ${bar} ${frag_reader} -q 'pat'" \
  "${frag_writer} '%s' \"\$V\" ${bar} ${frag_reader} --quiet 'pat'" \
  >"$tmp/seed-banned.sh"
seed_hits="$(scan_hits "$tmp/seed-banned.sh")"
seed_count="$(rg -c '' <<<"$seed_hits" || true)"
if [ "$seed_count" -eq 2 ]; then
  ok "detector flags a planted piped-to-quiet violation (short and long form)"
else
  no "detector missed a planted piped-to-quiet violation -- the guard is vacuous"
fi

# Seeded GREEN: the safe shapes must stay silent. A file-argument quiet
# match uses no pipe, and the -o reader drains stdin, so neither is racy.
printf '%s\n' \
  "${frag_reader} -q 'pat' -- \"\$file\"" \
  "${frag_writer} '%s' \"\$V\" ${bar} ${frag_reader} -o 'pat'" \
  >"$tmp/seed-safe.sh"
if [ -z "$(scan_hits "$tmp/seed-safe.sh")" ]; then
  ok "detector stays silent on the safe shapes (file argument, draining reader)"
else
  no "detector false-positives on a safe shape -- the guard would cry wolf"
fi

# The real scan: no banned line in any canon script, including this guard.
targets=()
for rel in "${canon_scripts[@]}"; do
  [ -f "$repo_root/$rel" ] && targets+=("$repo_root/$rel")
done
real_hits="$(scan_hits "${targets[@]}")"
if [ -z "$real_hits" ]; then
  ok "no piped-to-quiet assertion remains in the canon scripts"
else
  no "piped-to-quiet assertion reappeared in the canon scripts:"
  printf '%s\n' "$real_hits" >&2
fi

printf '\nagent-canon pipe-idiom guard: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
