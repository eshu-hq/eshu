#!/usr/bin/env bash
# Filename-stutter gate (issues #6627, #6821) -- enforces naming rules 2 and 3
# from docs/internal/naming.md on newly introduced paths.
#
# Rule 2, file stutter: a file stem must not repeat its leaf directory name.
# `semantic/entity/entity_intents.go` repeats `entity`: the directory already
# says where the file lives, so the extra word is glued naming-rule debt that
# every later reader pays. The #6736 semantic nest shipped exactly this shape
# and needed a follow-up rename to fix it; this gate catches it at commit time
# instead of review time. It applies to every file type, not only Go: docs,
# scripts, YAML and SQL repeat their directory name just as easily
# (`evidence/6821-evidence.md`).
#
# Rule 3, directory stutter: a newly introduced directory must not start or
# end with its parent directory's name. `query/queryauth` and
# `security/securityalert` are the glued compounds rule 3 forbids; nest them
# as `query/auth` and `security/alert`. The comparison is case-insensitive on
# the parent's whole name, so `query/query-auth`, `query/query_auth` and
# `query/auth_query` fail while `query/auth` passes. Parents that are
# repo-structure words rather than domain names (`go`, `internal`, `cmd`,
# `docs`, `scripts`, `specs`, `testdata`, `tests`) are skipped, as are
# dot-directories (`expected/codex/.codex`: named by the tool, not by us).
# Matching is tiered by the parent's length, because the short parents are the
# busiest domain dirs (`mcp`, `api`, `cli`, `aws`) where prefix glue really
# happens, yet a two- or three-letter parent is also routinely the first or
# last letters of an unrelated longer word:
#   - 1-2 characters: the parent must appear as a whole word, so `db/db-auth`
#     and `db/auth_db` fail while `db/nornicdb` and `parser/c/cpp` pass.
#   - exactly 3 characters: a whole word, or the child STARTS WITH the parent
#     (prefix only, never suffix): `api/apiauth`, `mcp/mcpserver`,
#     `sql/sqlstore` and `git/github` fail; `sql/postgresql` and
#     `net/dotnet` pass. (`git/github` is a prefix match and fails; nest it
#     as `github/` instead.)
#   - 4 or more characters: the child starts or ends with the parent.
# Everything at or below a `testdata` or `fixtures` directory is exempt from
# the directory check and from the file check for non-Go files; Go files
# there keep the file rule (see below). The directories above that root are
# still checked. Any path component named `fixtures` counts, first-party
# e2e helpers included.
# A directory is "new" only when it does not exist at
# the merge base (HEAD for --staged), so touching a file inside a legacy
# stuttering directory never re-flags the directory.
#
# The file rule is exact-word, not stemming: `-` and `_` are the same
# separator, and the leaf directory's words must appear as a contiguous run of
# the stem's words. `run-locally/run-locally-compose.md` and
# `ci_gates/ci_gates_extra.sh` fail; `satisfied_by_intents.go` under
# `satisfaction/` does NOT flag (`satisfied` != `satisfaction`). Comparison is
# case-insensitive, extension included. Exemptions, each idiomatic rather than
# stutter:
#   - stem == leaf dir (`catalog/catalog.go`, `openapi/openapi.yaml`): the
#     package-root or same-named front-door file.
#   - README.md, AGENTS.md, CLAUDE.md: conventional names that GitHub and
#     agent harnesses look up by name. A committed CLAUDE.md is refused
#     separately by verify-agent-canon.sh; it stays here so the two gates
#     never report the same file twice.
#   - doc.go: the godoc file, named by the toolchain.
#   - non-Go files at or below a `testdata` or `fixtures` directory, and
#     every directory there: fixture corpora mirror third-party language
#     conventions (Ruby `*_test.rb`, pytest `test_*.py`, Dart `*_test.dart`)
#     that we do not control. Go files there keep their pre-#6821 behaviour.
# Only newly introduced names are checked -- Added and Renamed destinations --
# so legacy family conventions (`correlation/rules/*_rules.go`,
# `parser/elixir/engine_elixir_*`, `cmd/*` binary prefixes, the legacy
# stuttering directories) never block unrelated work.
#
# Modes:
#   (default)      scan Added + Renamed destinations in the merge-base
#                  range plus staged files. Used by local promotion
#                  (registry local.command), where the changes are normally
#                  already committed and the index is clean, so --staged
#                  alone would false-green.
#   --staged       scan files staged in the index (Added + Renamed).
#                  Used by pre-commit only, the point where the name is
#                  introduced.
#   --files <f..>  scan explicit repo-relative paths; every directory in them
#                  counts as new. Used by tests.
#   --range <base> scan Added + Renamed destinations in <base>...HEAD.
#                  Used by CI so web edits and bot commits are covered too.
#
# Exit 0 when clean; 1 listing each offending path and why; 2 on usage or
# unresolvable-base errors (fail closed, never green on an unscanned tree).
set -euo pipefail

repo_root="${ESHU_STUTTER_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi
cd "$repo_root"

mode="default"
range_base=""
paths=()
case "${1:-}" in
  --staged) mode="staged" ;;
  --range)
    mode="range"
    range_base="${2:-}"
    if [ -z "$range_base" ]; then
      echo "usage: $(basename "$0") --range <base>" >&2
      exit 2
    fi
    ;;
  --files) mode="files"; shift; paths=("$@") ;;
  "") mode="default" ;;
  -*) echo "usage: $(basename "$0") [--staged | --files <f>... | --range <base> | <f>...]" >&2; exit 2 ;;
  *) mode="files"; paths=("$@") ;;
esac

# The merge-base upstream for default mode. Overridable for tests; mirrors
# the factschema-diff precedent of diffing against the integration branch.
upstream="${ESHU_STUTTER_UPSTREAM:-origin/main}"

# Materialize the candidate list in the main shell before scanning. A
# failing producer (bogus base, disconnected history) must fail the gate,
# but an exit inside the process substitution feeding the scan loop would
# die in the subshell, hand the loop EOF, and report a clean tree.
candidates_file="$(mktemp)"
trap 'rm -f "$candidates_file"' EXIT
base_ref=""
case "$mode" in
  staged)
    git diff --cached --name-status --diff-filter=AR -z >"$candidates_file"
    # A directory is new when the last commit lacks it. A repo with no
    # commit yet has no base tree, so every directory counts as new.
    if git rev-parse --verify --quiet "HEAD^{commit}" >/dev/null; then
      base_ref="HEAD"
    fi
    ;;
  range)
    if ! git rev-parse --verify --quiet "$range_base^{commit}" >/dev/null; then
      printf 'filename-stutter: cannot resolve range base %q\n' "$range_base" >&2
      exit 2
    fi
    if ! git diff --name-status "$range_base...HEAD" --diff-filter=AR -z >"$candidates_file"; then
      printf 'filename-stutter: cannot diff range %q...HEAD (no merge base?)\n' "$range_base" >&2
      exit 1
    fi
    base_ref="$(git merge-base "$range_base" HEAD)"
    ;;
  default)
    if ! base_ref="$(git merge-base HEAD "$upstream" 2>/dev/null)"; then
      printf 'filename-stutter: cannot resolve merge base with %q\n' "$upstream" >&2
      exit 2
    fi
    {
      git diff --name-status "$base_ref...HEAD" --diff-filter=AR -z
      git diff --cached --name-status --diff-filter=AR -z
    } >"$candidates_file"
    ;;
  files) printf '%s\0' "${paths[@]}" >"$candidates_file" ;;
esac

# Lowercase the whole NUL-delimited candidate list with ONE tr instead of a
# fork per path: the gate scans every added path of a PR, and a fork per path
# costs ~22ms each on macOS. ASCII-only (C locale) keeps byte lengths and NUL
# field boundaries identical to the original list, so the two files are read
# in lockstep below and diagnostics keep each path's original case.
lower_file="$(mktemp)"
trap 'rm -f "$candidates_file" "$lower_file"' EXIT
LC_ALL=C tr 'A-Z' 'a-z' <"$candidates_file" >"$lower_file"

# Parents that are repo-structure words, not domain names; a child repeating
# them (`internal/internalapi`) is not the glued-compound smell rule 3 targets.
exempt_parent() {
  case "$1" in
    go | internal | cmd | docs | scripts | specs | testdata | tests) return 0 ;;
  esac
  return 1
}

# dir_is_new <dir>: true unless <dir> is a directory (tree) in the base tree.
# A file or symlink at the same path does not count as the directory existing:
# `git cat-file -e` succeeds for any object, so a tracked file `query/queryauth`
# replaced by a directory of that name would otherwise pass as pre-existing.
# With no base tree (--files, first commit) every directory counts as new.
dir_is_new() {
  [ -z "$base_ref" ] && return 0
  [ "$(git cat-file -t "$base_ref:$1" 2>/dev/null)" != "tree" ]
}

# Directories already examined (newline-delimited), so a directory shared by
# many added files is looked up and reported once.
seen_dirs=$'\n'
failures=0

# is_fixture_root <lowercased component>: directories that hold fixture
# corpora; the component and everything below it is exempt.
is_fixture_root() {
  case "$1" in
    testdata | fixtures) return 0 ;;
  esac
  return 1
}

# dir_stutters <lowercased name> <lowercased parent>: rule 3 text match,
# tiered by the parent's length (owner ruling, #6821):
#   1-2 chars  the parent must appear as a whole `_`/`-` separated word.
#   3 chars    a whole word, OR the name starts with the parent (prefix only).
#   4+ chars   the name starts or ends with the parent.
dir_stutters() {
  local name="${1//-/_}" parent="${2//-/_}"
  case "_${name}_" in *"_${parent}_"*) return 0 ;; esac
  case "${#parent}" in
    1 | 2) return 1 ;;
    3) [[ "$name" == "$parent"* ]] ;;
    *) [[ "$name" == "$parent"* || "$name" == *"$parent" ]] ;;
  esac
}

# check_dirs <path> <lowercased path>: rule 3 over every directory component
# above any fixture root. Adds one diagnostic and one failure per newly
# introduced stuttering directory. Builtins only in the scan loop.
check_dirs() {
  local path="$1" lpath="$2" dir="" name="" parent="" lname="" lparent="" i
  local parts lparts
  case "$path" in */*) ;; *) return 0 ;; esac
  IFS='/' read -r -a parts <<<"${path%/*}"
  IFS='/' read -r -a lparts <<<"${lpath%/*}"
  for ((i = 0; i < ${#parts[@]}; i++)); do
    name="${parts[$i]}"
    lname="${lparts[$i]}"
    # The fixture root and everything below it is exempt; the ancestors above
    # it were already checked by earlier iterations.
    is_fixture_root "$lname" && return 0
    if [ "$i" = 0 ]; then
      dir="$name"
      continue
    fi
    parent="${parts[$((i - 1))]}"
    dir="$dir/$name"
    lparent="${lparts[$((i - 1))]}"
    exempt_parent "$lparent" && continue
    # Dot-directories are named by external tools (`.codex`, `.aider`,
    # `.github`), not by us, so the parent-name overlap is not glue.
    case "$name" in .*) continue ;; esac
    dir_stutters "$lname" "$lparent" || continue
    case "$seen_dirs" in *$'\n'"$dir"$'\n'*) continue ;; esac
    seen_dirs="$seen_dirs$dir"$'\n'
    if dir_is_new "$dir"; then
      printf 'filename-stutter: %s introduces directory %s that repeats its parent %s (naming rule 3: nest, do not glue)\n' "$path" "$dir" "$parent" >&2
      failures=$((failures + 1))
    fi
  done
}

# check_file <path> <lowercased path>: rule 2 for one path, any file type.
check_file() {
  local path="$1" lpath="$2" file lfile lbase ldir lleaf
  case "$path" in */*) ;; *) return 0 ;; esac
  file="${path##*/}"
  lfile="${lpath##*/}"
  ldir="${lpath%/*}"
  lleaf="${ldir##*/}"
  case "$file" in
    README.md | AGENTS.md | CLAUDE.md | doc.go) return 0 ;;
  esac
  # Strip one extension (dotfiles and extensionless names keep theirs), then
  # a Go test suffix. Done on the lowercased name so `.PNG` strips too.
  lbase="$lfile"
  case "$lfile" in
    ?*.*) lbase="${lfile%.*}" ;;
  esac
  case "$lfile" in
    *.go) lbase="${lbase%_test}" ;;
    *) case "/$lpath/" in */testdata/* | */fixtures/*) return 0 ;; esac ;;
  esac
  # `-` and `_` are one separator.
  lbase="${lbase//-/_}"
  lleaf="${lleaf//-/_}"
  # Idiomatic package-root file: catalog/catalog.go is the package's own
  # front door, not stutter.
  if [ "$lbase" = "$lleaf" ]; then
    return 0
  fi
  # The leaf's words repeat as a contiguous run of the stem's words.
  case "_${lbase}_" in
    *"_${lleaf}_"*)
      printf 'filename-stutter: %s repeats directory %s\n' "$path" "${ldir##*/}" >&2
      failures=$((failures + 1))
      ;;
  esac
}

exec 3<"$candidates_file" 4<"$lower_file"
while IFS= read -r -d '' status <&3; do
  IFS= read -r -d '' lstatus <&4
  path="$status"
  lpath="$lstatus"
  case "$mode" in
    staged | range | default)
      # --name-status -z emits <status>\0<path>\0, or for renames
      # <status>\0<old>\0<new>\0: the destination (last field) is the
      # introduced name.
      if [[ "$status" =~ ^R ]]; then
        IFS= read -r -d '' _old <&3
        IFS= read -r -d '' _lold <&4
      fi
      IFS= read -r -d '' path <&3
      IFS= read -r -d '' lpath <&4
      ;;
  esac
  check_dirs "$path" "$lpath"
  check_file "$path" "$lpath"
done
exec 3<&- 4<&-

if [ "$failures" != "0" ]; then
  printf 'filename-stutter: %d offending path(s) (naming rules 2 and 3: never repeat the directory name in a file name, never glue a directory name onto its parent'"'"'s)\n' "$failures" >&2
  exit 1
fi
exit 0
