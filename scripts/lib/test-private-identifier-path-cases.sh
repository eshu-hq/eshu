# shellcheck shell=bash
# shellcheck disable=SC2154 # repo_root, tmp, pi_* and ok/no come from the sourcing mirror
# test-private-identifier-path-cases.sh: seeded RED/GREEN cases for how
# scripts/verify-no-private-identifiers.sh REPORTS the path of a hit. Sourced by
# scripts/test-verify-agent-hygiene.sh after test-private-identifier-cases.sh,
# whose pi_* helpers and identifier fragments it reuses (the cases file is at
# its line cap, so the path cases live beside it).
#
# git quotes a path that holds a non-ASCII character, a double quote, a
# backslash or a control character, and prints the destination with a "b/"
# prefix that config can change. A location the operator cannot paste back is a
# bad failure message, and a prefix strip that eats a real "b/" directory names
# the wrong file. Each case plants a token (built from fragments, so this file
# carries no literal deny token) in a file with an awkward name and requires the
# exact literal location, once, and no echo of the match.

# "cafe" with an acute e, spelled as bytes so no editor can renormalise it.
pi_p_utf8=$'caf\303\251'

# pi_loc_once <label> <want>: the last gate output holds <want> as one whole
# line, holds exactly one location line, and shows no octal escape or quote
# that git's C-style quoting would leave behind.
pi_loc_once() {
  local label="$1" want="$2" n_want n_loc
  n_want="$(rg -c -x -F -e "$want" "$tmp/pi.out" || true)"
  n_loc="$(rg -c -e ':[0-9]+$' "$tmp/pi.out" || true)"
  if [ "${n_want:-0}" = 1 ] && [ "${n_loc:-0}" = 1 ] && ! rg -q -e '\\[0-7]{3}' "$tmp/pi.out"; then
    ok "private-identifier reports a literal path once: $label"
  else
    no "private-identifier must report '$want' once as a whole line: $label"
  fi
}

# pi_path_range <name> <path> <want> <label> [<git-config-key>=<value>]: a
# repository that adds a token line to <path>, scanned in range mode.
pi_path_range() {
  local d="$tmp/pi-path-$1"
  pi_new_repo "$d"
  if [ -n "${5:-}" ]; then
    git -C "$d" config "${5%%=*}" "${5#*=}"
  fi
  pi_commit "$d" "$2" "Measured on the ${pi_qa_h} cluster."
  pi_expect red "path case: $4" pi_range "$d"
  pi_loc_once "$4" "$3"
  pi_no_echo "path case: $4" "$pi_qa_h"
}

# pi_path_staged <name> <path> <want> <label> [<git-config-key>=<value>]: the
# same plant, staged.
pi_path_staged() {
  local d="$tmp/pi-pathst-$1"
  pi_new_repo "$d"
  if [ -n "${5:-}" ]; then
    git -C "$d" config "${5%%=*}" "${5#*=}"
  fi
  mkdir -p "$d/$(dirname "$2")"
  printf 'Seen on the %s cluster.\n' "$pi_qa_h" >"$d/$2"
  git -C "$d" add -A
  pi_expect red "path case: $4 (--staged)" \
    env -u ESHU_PRIVATE_IDENTIFIER_FILE ESHU_PRIVATE_IDENTIFIER_REPO_ROOT="$d" "$pi_gate" --staged
  pi_loc_once "$4 (--staged)" "$3"
  pi_no_echo "path case: $4 (--staged)" "$pi_qa_h"
}

# --- non-ASCII names: no quotes, no octal escapes, no "b/" ---
pi_path_range utf8 "${pi_p_utf8}-notes.md" "${pi_p_utf8}-notes.md:1" "non-ASCII file name"
pi_path_range utf8-dir "docs/${pi_p_utf8}/n.md" "docs/${pi_p_utf8}/n.md:1" "non-ASCII directory"
pi_path_staged utf8 "${pi_p_utf8}-notes.md" "${pi_p_utf8}-notes.md:1" "non-ASCII file name"

# --- a name git quotes for another reason ---
pi_path_range space "docs/my notes.md" "docs/my notes.md:1" "space in the name"
pi_path_range quote 'docs/say "hi".md' 'docs/say "hi".md:1' "double quote in the name"
pi_path_staged quote 'docs/say "hi".md' 'docs/say "hi".md:1' "double quote in the name"
pi_path_range backslash 'docs/a\b.md' 'docs/a\b.md:1' "backslash in the name"
pi_path_range utf8-quote "${pi_p_utf8}/say \"hi\".md" "${pi_p_utf8}/say \"hi\".md:1" \
  "non-ASCII directory and a double quote"

# --- a real directory called b is never stripped, under any prefix config ---
pi_path_range realb b/x.md "b/x.md:1" "real b/ directory"
pi_path_range realb-noprefix b/x.md "b/x.md:1" "real b/ directory, diff.noprefix=true" diff.noprefix=true
pi_path_range realb-mnemonic b/x.md "b/x.md:1" "real b/ directory, diff.mnemonicPrefix=true" \
  diff.mnemonicPrefix=true
pi_path_range mnemonic docs/n.md "docs/n.md:1" "plain path, diff.mnemonicPrefix=true" \
  diff.mnemonicPrefix=true
pi_path_range noprefix docs/n.md "docs/n.md:1" "plain path, diff.noprefix=true" diff.noprefix=true
pi_path_staged mnemonic docs/n.md "docs/n.md:1" "plain path, diff.mnemonicPrefix=true" \
  diff.mnemonicPrefix=true
pi_path_staged realb-noprefix b/x.md "b/x.md:1" "real b/ directory, diff.noprefix=true" diff.noprefix=true
pi_path_range quotepath-off "${pi_p_utf8}.md" "${pi_p_utf8}.md:1" "non-ASCII name, core.quotePath=false" \
  core.quotePath=false

# --- rename detection (-M) still reports a renamed non-ASCII file once ---
# diff.renames=false makes the explicit -M the only thing that detects the move.
d="$tmp/pi-path-rename"
pi_new_repo "$d" "docs/${pi_p_utf8}-old.md" "$(printf 'one\nMeasured on %s\nthree' "$pi_qa_h")"
git -C "$d" config diff.renames false
mkdir -p "$d/moved"
git -C "$d" mv "docs/${pi_p_utf8}-old.md" "moved/${pi_p_utf8}-new.md"
git -C "$d" commit -q -m "docs: move the note"
pi_expect green "path case: pure move of a non-ASCII file that carries a token" pi_range "$d"
printf 'Also seen on %s.\n' "$pi_qa_h" >>"$d/moved/${pi_p_utf8}-new.md"
git -C "$d" add -A
git -C "$d" commit -q -m "docs: extend the moved note"
pi_expect red "path case: added token line of a moved non-ASCII file" pi_range "$d"
pi_loc_once "moved non-ASCII file" "moved/${pi_p_utf8}-new.md:4"

# --- private denylist mode reports the same literal location ---
d="$tmp/pi-path-private"
pi_new_repo "$d"
pi_commit "$d" "docs/${pi_p_utf8}-n.md" "repo ${pi_secret} regressed."
pi_expect red "path case: private denylist hit in a non-ASCII file" \
  pi_range_private "$d" "$tmp/pi-deny.txt"
pi_loc_once "private denylist, non-ASCII file" "docs/${pi_p_utf8}-n.md:1"
if rg -q -F -e "$pi_secret" "$tmp/pi.out"; then
  no "private-identifier must not echo a private match in a non-ASCII path case"
else
  ok "private-identifier does not echo a private match in a non-ASCII path case"
fi

# --- this file must not carry a deny token itself ---
if [ "$(pi_lib_hits "$repo_root/scripts/lib/test-private-identifier-path-cases.sh")" -eq 0 ]; then
  ok "private-identifier path cases library carries no deny token"
else
  no "private-identifier path cases library must carry no deny token"
fi
