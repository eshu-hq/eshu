# shellcheck shell=bash
# shellcheck disable=SC2154 # repo_root and tmp come from the sourcing test mirror
# test-private-identifier-cases.sh: seeded RED/GREEN cases for
# scripts/verify-no-private-identifiers.sh. Sourced by
# scripts/test-verify-agent-hygiene.sh, which supplies $repo_root, $tmp, ok()
# and no().
#
# Every fixture builds its identifier strings from fragments at run time, and
# no label or comment spells a token, so this file carries no literal deny
# token: the last block scans this file with the shared pattern and requires
# zero hits. Each case runs the gate in a throwaway git repository: RED cases
# plant a violation and must exit non-zero, GREEN cases must exit zero.

pi_gate="$repo_root/scripts/verify-no-private-identifiers.sh"
# shellcheck source=scripts/lib/private-identifier-pattern.sh
source "$repo_root/scripts/lib/private-identifier-pattern.sh"

# Identifier fragments. The hyphen, underscore and CamelCase spellings of the
# QA and production environment names, and the organization name.
pi_qa_h="ops""-qa"
pi_prod_h="ops""-prod"
pi_qa_u="ops""_qa"
pi_qa_camel="Ops""Qa"
pi_org="boats""group"
pi_qa_camel_upper="ops""QA"
pi_qa_camel_lower="ops""Qa"
pi_prod_camel="Ops""Prod"
pi_org_sep="Boats"" Group"

# pi_new_repo <dir> [<file> <content>]: create a repository whose first commit
# holds README.md and, when given, <file> with <content>.
pi_new_repo() {
  git init -q "$1"
  git -C "$1" config user.email "fixture@example.invalid"
  git -C "$1" config user.name "fixture"
  git -C "$1" config commit.gpgsign false
  printf 'base\n' >"$1/README.md"
  if [ "$#" -ge 3 ]; then
    mkdir -p "$1/$(dirname "$2")"
    printf '%s\n' "$3" >"$1/$2"
  fi
  git -C "$1" add -A
  git -C "$1" commit -q -m "fixture: base"
}

# pi_commit <dir> <file> <content> [<message>]: add <content> as a new line of
# <file> in its own commit.
pi_commit() {
  mkdir -p "$1/$(dirname "$2")"
  printf '%s\n' "$3" >>"$1/$2"
  git -C "$1" add -A
  git -C "$1" commit -q -m "${4:-fixture: change}"
}

# pi_range <dir>: run the gate in range mode against the repo's first commit,
# with no private denylist unless the caller exports ESHU_PRIVATE_IDENTIFIER_FILE
# through pi_range_private.
pi_range() {
  local base
  base="$(git -C "$1" rev-list --max-parents=0 HEAD)"
  env -u ESHU_PRIVATE_IDENTIFIER_FILE \
    ESHU_PRIVATE_IDENTIFIER_REPO_ROOT="$1" ESHU_PRIVATE_IDENTIFIER_BASE="$base" \
    "$pi_gate"
}

# pi_range_private <dir> <denylist>: range mode with a private denylist.
pi_range_private() {
  local base
  base="$(git -C "$1" rev-list --max-parents=0 HEAD)"
  ESHU_PRIVATE_IDENTIFIER_REPO_ROOT="$1" ESHU_PRIVATE_IDENTIFIER_BASE="$base" \
    ESHU_PRIVATE_IDENTIFIER_FILE="$2" "$pi_gate"
}

# pi_range_base <dir> <base>: range mode against an explicit base ref.
pi_range_base() {
  env -u ESHU_PRIVATE_IDENTIFIER_FILE \
    ESHU_PRIVATE_IDENTIFIER_REPO_ROOT="$1" ESHU_PRIVATE_IDENTIFIER_BASE="$2" \
    "$pi_gate"
}

# pi_expect <red|green> <label> <command...>: run the command and compare its
# exit status. Output is kept in $tmp/pi.out for follow-up assertions.
pi_expect() {
  local want="$1" label="$2"
  shift 2
  if "$@" >"$tmp/pi.out" 2>&1; then
    if [ "$want" = green ]; then ok "private-identifier $label"; else no "private-identifier should fail: $label"; fi
  else
    # A RED case counts only when the gate itself reported the failure, so a
    # missing or crashing script cannot read as a caught violation.
    if [ "$want" = red ] && rg -q '^verify-no-private-identifiers: ' "$tmp/pi.out"; then
      ok "private-identifier fails: $label"
    else
      no "private-identifier should pass (or the gate did not report): $label"
    fi
  fi
}

# pi_no_echo <label> <token>: the last gate output in $tmp/pi.out must not
# contain <token> in any casing. A public match is reported as a location
# (path:line or commit-message line N), never as the offending text.
pi_no_echo() {
  if rg -q -F -i -e "$2" "$tmp/pi.out"; then
    no "private-identifier must not echo the matched text: $1"
  else
    ok "private-identifier does not echo the matched text: $1"
  fi
}

# --- RED: a planted identifier in added content ---
d="$tmp/pi-red-md"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on the ${pi_qa_h} cluster."
pi_expect red "environment token in an added .md line" pi_range "$d"
if rg -q 'docs/notes\.md:1' "$tmp/pi.out"; then
  ok "private-identifier names the offending file and line"
else
  no "private-identifier must name docs/notes.md:1 in its output"
fi
pi_no_echo "public match in added content" "$pi_qa_h"

d="$tmp/pi-red-go"
pi_new_repo "$d"
pi_commit "$d" internal/x/x.go "// Tuned against ${pi_prod_h} traffic."
pi_expect red "environment token in an added Go comment" pi_range "$d"

d="$tmp/pi-red-underscore"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "const scenario = \"${pi_qa_u}_scale\""
pi_expect red "underscore form with a suffix" pi_range "$d"

d="$tmp/pi-red-camel"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "func TestBloatTerraform${pi_qa_camel}ScaleLive() {}"
pi_expect red "CamelCase identifier form" pi_range "$d"

d="$tmp/pi-red-org"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Contact the ${pi_org} platform team."
pi_expect red "organization token in an added line" pi_range "$d"

d="$tmp/pi-red-org-sep"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Owned by ${pi_org_sep} infrastructure."
pi_expect red "organization token with a separator and capitals" pi_range "$d"

d="$tmp/pi-red-message"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "A clean line." "fix: measured on ${pi_qa_h}"
pi_expect red "environment token in a commit message" pi_range "$d"
pi_no_echo "public match in a commit message" "$pi_qa_h"
if rg -q 'commit-message line 1$' "$tmp/pi.out"; then
  ok "private-identifier names the commit-message line only"
else
  no "private-identifier must print 'commit-message line 1' for a message hit"
fi

d="$tmp/pi-red-staged"
pi_new_repo "$d"
printf 'Seen on %s.\n' "$pi_prod_h" >"$d/staged.md"
git -C "$d" add staged.md
pi_expect red "environment token in staged content" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE ESHU_PRIVATE_IDENTIFIER_REPO_ROOT="$d" \
  "$pi_gate" --staged
pi_no_echo "public match in staged content" "$pi_prod_h"
if rg -q 'staged\.md:1$' "$tmp/pi.out"; then
  ok "private-identifier names the staged file and line only"
else
  no "private-identifier must print 'staged.md:1' for a staged hit"
fi

printf 'fix: measured on %s\n' "$pi_qa_h" >"$tmp/pi-msg-red"
pi_expect red "environment token in a commit-message file" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE "$pi_gate" --message "$tmp/pi-msg-red"
pi_no_echo "public match in a commit-message file" "$pi_qa_h"
if rg -q 'commit-message line 1$' "$tmp/pi.out"; then
  ok "private-identifier names the message-file line only"
else
  no "private-identifier must print 'commit-message line 1' for a message-file hit"
fi

# --- RED: private mode ---
# The denylist lives outside the repository. One line is a blank, one a
# comment, one a real pattern; only the pattern may match.
pi_secret="zz""9f3a1c0d"
printf '# local denylist\n\n%s\n' "$pi_secret" >"$tmp/pi-deny.txt"
d="$tmp/pi-red-private"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "repo ${pi_secret} regressed."
pi_expect red "private denylist hit in an added line" pi_range_private "$d" "$tmp/pi-deny.txt"
if rg -q "$pi_secret" "$tmp/pi.out"; then
  no "private-identifier must not echo a private match"
else
  ok "private-identifier does not echo a private match"
fi

pi_expect red "private denylist file that is missing" \
  pi_range_private "$d" "$tmp/pi-no-such-denylist.txt"

d="$tmp/pi-red-private-inrepo"
pi_new_repo "$d"
printf '%s\n' "$pi_secret" >"$d/denylist.txt"
pi_expect red "private denylist file inside the repository" \
  pi_range_private "$d" "$d/denylist.txt"

# --- GREEN ---
d="$tmp/pi-green-clean"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on the QA environment."
pi_expect green "passes a clean commit" pi_range "$d"

d="$tmp/pi-green-removal"
pi_new_repo "$d" docs/notes.md "Measured on ${pi_qa_h} and ${pi_org}."
: >"$d/docs/notes.md"
git -C "$d" add -A
git -C "$d" commit -q -m "docs: drop the environment names"
pi_expect green "passes a commit that only removes a token line" pi_range "$d"

d="$tmp/pi-green-lib"
pi_new_repo "$d"
pi_commit "$d" scripts/lib/private-identifier-pattern.sh "# covers ${pi_qa_h} and ${pi_org}"
pi_expect green "passes an edit to the pattern lib itself" pi_range "$d"

d="$tmp/pi-green-shape"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Repository r_0a1b2c3d regressed; account 123456789012."
pi_expect green "passes the generic r_<8hex> shape and a 12-digit number" pi_range "$d"

d="$tmp/pi-green-devops"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Use the devops-prod runbook and the DevOpsQa* naming rule."
pi_expect green "passes a word that only ends in the token letters" pi_range "$d"

d="$tmp/pi-green-private-unset"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Nothing sensitive here."
pi_expect green "passes with the private denylist unset" pi_range "$d"
if rg -q 'private denylist: not configured' "$tmp/pi.out"; then
  ok "private-identifier says the private denylist is not configured"
else
  no "private-identifier must print 'private denylist: not configured'"
fi

printf 'feat: a clean message\n' >"$tmp/pi-msg-green"
pi_expect green "passes a clean commit-message file" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE "$pi_gate" --message "$tmp/pi-msg-green"

pi_expect green "private denylist configured and no hit" pi_range_private \
  "$tmp/pi-green-clean" "$tmp/pi-deny.txt"

# --- F1: a git failure must fail the gate, never read as a clean scan ---
d="$tmp/pi-red-badbase"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on ${pi_qa_h}."
pi_expect red "unresolvable base ref fails closed" \
  pi_range_base "$d" refs/heads/does-not-exist
if rg -q 'git merge-base' "$tmp/pi.out"; then
  ok "private-identifier names the failed git command"
else
  no "private-identifier must name the failed git command"
fi
pi_expect red "unknown 40-hex base fails closed" \
  pi_range_base "$d" 0000000000000000000000000000000000000000
mkdir -p "$tmp/pi-not-a-repo"
pi_expect red "--staged outside a git repository fails closed" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE ESHU_PRIVATE_IDENTIFIER_REPO_ROOT="$tmp/pi-not-a-repo" \
  "$pi_gate" --staged

# --- F2: CamelCase spellings and the spaced form ---
d="$tmp/pi-red-camel-qa-upper"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "func TestBloatTerraformOps""QAScaleLive() {}"
pi_expect red "camel casing 1 (upper initialism after a capital)" pi_range "$d"

d="$tmp/pi-red-camel-lower-first"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "var ${pi_qa_camel_upper}Host = 1"
pi_expect red "camel casing 2 (lower first word, upper initialism)" pi_range "$d"

d="$tmp/pi-red-camel-lower-qa"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "var ${pi_qa_camel_lower}Host = 1"
pi_expect red "camel casing 3 (lower first word, capitalised second)" pi_range "$d"

d="$tmp/pi-red-camel-prod"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "var ${pi_prod_camel}Host = 1"
pi_expect red "camel casing 4 (production name, capitalised words)" pi_range "$d"

d="$tmp/pi-red-spaced"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on the ops"" QA cluster."
pi_expect red "spaced separator form" pi_range "$d"

d="$tmp/pi-green-devops-camel"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "func TestDevOps""QAHelper() { devOps""Prod := 1 }"
pi_expect green "DevOps CamelCase is not a hit" pi_range "$d"

# An unchanged legacy line next to a clean edit must pass: only added lines
# are scanned, so a fixture name already in the tree needs no exception.
d="$tmp/pi-green-unchanged-legacy"
pi_new_repo "$d" internal/x/legacy_test.go "var ${pi_qa_camel_upper}Platforms = 1"
pi_commit "$d" internal/x/legacy_test.go "var unrelated = 2"
pi_expect green "passes an unchanged legacy token line beside a clean edit" pi_range "$d"

# --- F3: git's own comment lines in an editor-made message are not content ---
printf 'feat: rename a note\n\n# Changes to be committed:\n#\trenamed:    7000-note-%s.md -> 7000-note-qa.md\n' \
  "$pi_qa_h" >"$tmp/pi-msg-comment"
pi_expect green "passes a token that only sits on a git comment line" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE "$pi_gate" --message "$tmp/pi-msg-comment"
printf 'feat: measured on %s\n\n# renamed: %s\n' "$pi_qa_h" "$pi_qa_h" >"$tmp/pi-msg-comment-red"
pi_expect red "still fails a token on a non-comment line beside comment lines" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE "$pi_gate" --message "$tmp/pi-msg-comment-red"
# The reported line number counts the file's own lines, comment lines included.
printf '# a status comment\n# another\nfeat: measured on %s\n' "$pi_qa_h" >"$tmp/pi-msg-comment-num"
pi_expect red "a token after comment lines is reported at its file line" \
  env -u ESHU_PRIVATE_IDENTIFIER_FILE "$pi_gate" --message "$tmp/pi-msg-comment-num"
if rg -q 'commit-message line 3$' "$tmp/pi.out"; then
  ok "private-identifier numbers a message line by its file line"
else
  no "private-identifier must print 'commit-message line 3' after two comment lines"
fi

# --- F4: rename detection (-M) is what makes a move of a legacy file clean ---
# git diff detects renames by default, so a mutation that drops -M from the gate
# would stay green. diff.renames=false in the fixture makes the explicit -M the
# only thing that detects the move.
d="$tmp/pi-green-move"
pi_new_repo "$d" docs/old.md "$(printf 'one\nMeasured on %s\nthree' "$pi_qa_h")"
git -C "$d" config diff.renames false
mkdir -p "$d/docs/moved"
git -C "$d" mv docs/old.md docs/moved/new.md
git -C "$d" commit -q -m "docs: move the note"
pi_expect green "passes a pure move of a file that carries a token" pi_range "$d"

d="$tmp/pi-red-move-add"
pi_new_repo "$d" docs/old.md "$(printf 'one\nMeasured on %s\nthree' "$pi_qa_h")"
git -C "$d" config diff.renames false
mkdir -p "$d/docs/moved"
git -C "$d" mv docs/old.md docs/moved/new.md
printf 'Also seen on %s.\n' "$pi_qa_h" >>"$d/docs/moved/new.md"
git -C "$d" add -A
git -C "$d" commit -q -m "docs: move and extend the note"
pi_expect red "flags the added token line of a moved file" pi_range "$d"
if rg -q 'docs/moved/new\.md:4$' "$tmp/pi.out" && ! rg -q 'docs/moved/new\.md:2$' "$tmp/pi.out"; then
  ok "private-identifier flags only the added line of a moved file"
else
  no "private-identifier must flag docs/moved/new.md:4 and not the moved line 2"
fi

# --- T3: joined spellings in other casings, and the spellings that stay clean ---
# Each RED line is a joined spelling of an environment name that the CamelCase
# arm used to miss. The fragments keep this file free of the contiguous token.
d="$tmp/pi-red-joined-caps"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "const OPS""QA_HOST = 1"
pi_expect red "joined all-caps QA name" pi_range "$d"

d="$tmp/pi-red-joined-caps-prod"
pi_new_repo "$d"
pi_commit "$d" internal/x/x_test.go "const OPS""PROD_HOST = 1"
pi_expect red "joined all-caps production name" pi_range "$d"

d="$tmp/pi-red-joined-sentence"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on Ops""qa today."
pi_expect red "joined sentence-case QA name" pi_range "$d"

d="$tmp/pi-red-joined-sentence-prod"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on Ops""prod today."
pi_expect red "joined sentence-case production name" pi_range "$d"

# One repository per spelling: a line that carries several spellings would pass
# on any one of them, so a missed casing would stay hidden.
pi_joined_n=0
for pi_form in "oPS|qa" "opS|QA" "oPs|Qa" "OpS|qA" "ops|pRod" "oPs|Prod" "opS|prod" "OPS|prod" "ops|PROD" "Ops|PRod"; do
  pi_joined_n=$((pi_joined_n + 1))
  d="$tmp/pi-red-joined-mixed-$pi_joined_n"
  pi_new_repo "$d"
  pi_commit "$d" internal/x/x_test.go "var x = ${pi_form%%|*}""${pi_form##*|} + 1"
  pi_expect red "joined mixed-case form $pi_joined_n" pi_range "$d"
done

d="$tmp/pi-red-joined-lower-word"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "Measured on the ops""qa cluster."
pi_expect red "joined all-lowercase word in prose" pi_range "$d"

# A hit in a commit message goes through the same shared pattern.
d="$tmp/pi-red-joined-message"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "A clean line." "fix: measured on OPS""QA"
pi_expect red "joined all-caps name in a commit message" pi_range "$d"

d="$tmp/pi-green-joined-clean"
pi_new_repo "$d"
pi_commit "$d" docs/notes.md "See DEV""OPS""QA, Dev""Ops""Prod, dev""ops""prod and the Ops""Product plan."
pi_expect green "passes DevOps-prefixed joined forms and a longer English word" pi_range "$d"

# The append-only ledger cites a measurement by an id that spells the name
# joined, lowercase, between hyphens. A new ledger row or evidence note adds
# such a line, and it must stay clean.
d="$tmp/pi-green-ledger-citation"
pi_new_repo "$d"
pi_commit "$d" docs/internal/evidence/9999-note.md "The p95 is 0.5 s (ledger:9999-ops""qa-directory-api-warm-p95)."
pi_commit "$d" docs/internal/evidence/9998-note.md "See [the run](9998-ops""qa-migration-run-20261003.md)."
pi_expect green "passes a lowercase joined name between hyphens (ledger id shape)" pi_range "$d"

# --- T1: this file must not carry a deny token itself ---
# pi_lib_hits <file>: print the number of lines of <file> that match the shared
# pattern. The mirror seeds one RED with a planted token, then requires the
# real cases library to have none.
pi_lib_hits() {
  local n
  n="$(rg -c -e "$PRIVATE_IDENTIFIER_PATTERN" "$1" || true)"
  printf '%s\n' "${n:-0}"
}
printf 'a label that spells %s in full\n' "$pi_qa_h" >"$tmp/pi-lib-planted.txt"
if [ "$(pi_lib_hits "$tmp/pi-lib-planted.txt")" -ge 1 ]; then
  ok "private-identifier lib scan catches a planted token"
else
  no "private-identifier lib scan must catch a planted token"
fi
if [ "$(pi_lib_hits "$repo_root/scripts/lib/test-private-identifier-cases.sh")" -eq 0 ]; then
  ok "private-identifier cases library carries no deny token"
else
  no "private-identifier cases library must carry no deny token"
fi
