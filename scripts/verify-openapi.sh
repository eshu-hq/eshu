#!/usr/bin/env bash
#
# verify-openapi.sh — diff mux.HandleFunc registrations against path definitions
# in openapi_paths_*.go files. Exit non-zero on any drift: a HandleFunc route
# without a matching openapi_paths entry, or an openapi_paths entry without a
# matching HandleFunc route.
#
# Scans go/internal/query/ and go/internal/serviceintelhttp/ for HandleFunc
# registrations. Cross-references against go/internal/query/openapi_paths_*.go.
#
# Self-contained: bash scripts/verify-openapi.sh exits 0 on a clean tree.
set -euo pipefail

repo_root="${ESHU_OPENAPI_VERIFY_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

query_dir="${repo_root}/go/internal/query"
si_dir="${repo_root}/go/internal/serviceintelhttp"

tmpdir="${ESHU_OPENAPI_VERIFY_TMPDIR:-}"
cleanup_tmp=0
if [ -z "$tmpdir" ]; then
  tmpdir="$(mktemp -d)"
  cleanup_tmp=1
else
  mkdir -p "$tmpdir"
fi
trap 'if [ "$cleanup_tmp" -eq 1 ]; then rm -rf "$tmpdir"; fi' EXIT

handlefunc_route_file="${tmpdir}/handlefunc_routes.txt"
: > "$handlefunc_route_file"

# Source directories to scan for HandleFunc registrations.
scan_dirs=()
[ -d "$query_dir" ] && scan_dirs+=("$query_dir")
[ -d "$si_dir" ] && scan_dirs+=("$si_dir")

# Collect all non-test, non-openapi Go files from scan dirs into a file list.
gofiles_tmp="${tmpdir}/gofiles.txt"
: > "$gofiles_tmp"
for dir in "${scan_dirs[@]}"; do
  # "|| true": unlike `find`, `rg --files` exits 1 (not 0) when a directory
  # has zero matching files -- under `set -e` that would abort the whole
  # script instead of just producing an empty file list for this dir.
  #
  # "--max-depth 1" and "!openapi_*.go" are semantic constraints, not bugs
  # fixed against an observed failure: neither $query_dir nor $si_dir has a
  # subdirectory today, and no non-"openapi_paths_*.go" file starting with
  # "openapi_" exists in either, so removing either flag changes nothing on
  # this repo (verified this session: 254/254 routes match with both flags
  # dropped, same as with them present) -- they are equivalent mutants on the
  # current corpus, not gaps in test coverage. They stay because the
  # constraint they express is real: a future subpackage under $query_dir
  # would otherwise be scanned depth-first for routes it does not own, and a
  # future "openapi_helpers.go" would otherwise be misread as a HandleFunc
  # source file. Left undocumented by a synthetic fixture (#5762 follow-up
  # P3-1) because a fixture that only reproduces this comment's own claim
  # -- and passes before AND after the flag is removed -- asserts nothing a
  # reviewer could not already read here.
  rg --files --max-depth 1 -g '*.go' -g '!*_test.go' -g '!openapi_*.go' \
    "$dir" 2>/dev/null \
  >> "$gofiles_tmp" || true
done

# When no Go files exist, rg with empty args would search $PWD. Use /dev/null
# as a safe no-op target so rg produces no output.
gofiles_args=()
if [ -s "$gofiles_tmp" ]; then
  while IFS= read -r f; do
    gofiles_args+=("$f")
  done < "$gofiles_tmp"
else
  gofiles_args=("/dev/null")
fi

# ── 1a. Direct string literal HandleFunc calls ──────────────────────────────
#     mux.HandleFunc("METHOD /path", ...)

rg --no-filename -o 'HandleFunc\("([A-Z]+) (/[^"]+)"' -r '$1 $2' \
  "${gofiles_args[@]}" 2>/dev/null \
>> "$handlefunc_route_file" || true

# ── 1b. Variable-based HandleFunc: resolve route constants ──────────────────
#
# Extract all variable names used in HandleFunc calls (not string literals),
# then look up their const/var definitions for "METHOD /path" values.

rg --no-filename -o 'HandleFunc\((\w+)[,) ]' -r '$1' \
  "${gofiles_args[@]}" 2>/dev/null \
| sort -u \
| while IFS= read -r varname; do
    [ -z "$varname" ] && continue
    rg --no-filename -o \
      '^\s*(const|var)\s+'"$varname"'\s*=\s*"([A-Z]+ /[a-z][^"]*)"' -r '$2' \
      "${gofiles_args[@]}" 2>/dev/null \
    | head -1
  done \
>> "$handlefunc_route_file" || true

# ── 1c. String concatenation: "METHOD "+variable ────────────────────────────
#
# Build a map of path-only constant name → path, then resolve concatenations.

path_constants_file="${tmpdir}/path_constants.txt"
rg --no-filename -o \
  '^\s*(const|var)\s+(\w+)\s*=\s*"(\/[a-z][^"]*)"' -r '$2 $3' \
  "${gofiles_args[@]}" 2>/dev/null \
> "$path_constants_file" || true

rg --no-filename -o 'HandleFunc\("([A-Z]+) "\+(\w+)' -r '$1 $2' \
  "${gofiles_args[@]}" 2>/dev/null \
| while IFS=' ' read -r method varname; do
    path=""
    while IFS=' ' read -r name val; do
      if [ "$name" = "$varname" ]; then
        path="$val"
        break
      fi
    done < "$path_constants_file"
    if [ -n "$path" ]; then
      echo "${method} ${path}"
    fi
  done \
>> "$handlefunc_route_file" || true

sort -u -o "$handlefunc_route_file" "$handlefunc_route_file"

# ── Known-drift exclusions ──────────────────────────────────────────────────
# `.github/openapi-known-drift.txt` lists routes intentionally excluded from the
# OpenAPI surface (e.g., documentation UIs). The verifier subtracts these from
# the drift report so the CI gate stays green on known gaps while catching new
# drift. One route per line, format "METHOD /path".

known_drift_file="${repo_root}/.github/openapi-known-drift.txt"

# ── Known-drift file self-validation ────────────────────────────────────────
#
# The known-drift file is a permanent-exclusion list, not a backlog (#5762).
# A route belongs here only when the scanner genuinely cannot resolve its
# METHOD/path, or when the route is not an API operation at all -- never
# because the fragment is simply missing so far. That third, unstated
# category is exactly what let POST /api/v0/code/visualize sit here under a
# "TODO(#3781): add ... fragment" comment for the six weeks since #3781 was
# filed: a deferral marker or a deferral phrase is self-refuting evidence
# that the entry is NOT a permanent, intentional exclusion, it is a
# known-but-deferred gap wearing an exclusion entry as camouflage. Four rules
# are enforced on every line before the file is used to suppress anything:
#
#   1. No COMMENT line may contain a TODO/TO-DO/FIXME/XXX/HACK/TBD/WIP
#      deferral marker (case-insensitive; matches the plural form too, e.g.
#      "TODOs", and the hyphenated/underscored/spaced "TO-DO" spelling). Only
#      comment lines are scanned, not route lines, so a route whose own path
#      happens to contain one of these words (e.g. "/todo-board",
#      "/cache/wipe") is never blocked by this rule -- it can still be
#      excluded here with a real justification comment.
#   2. No COMMENT line may contain a prose deferral phrase (case-insensitive)
#      such as "not written", "written yet", "pending", "predates", "later",
#      or "to be added/written" -- the same self-refuting signal spelled out
#      in words instead of a marker. This is a best-effort check against a
#      fixed phrase list, not a guarantee that every English deferral is
#      caught: a deferral phrased in ordinary prose outside this list, or a
#      false justification, will not be detected.
#   3. Every route entry must be preceded by its own non-empty, substantive
#      comment line: at least two whitespace-separated tokens, including one
#      alphabetic word of 4+ characters, so decoration such as "####" or
#      "# ---" cannot pass as a justification. Neither a bare route nor a
#      bare "#" can be appended silently, and one justification cannot be
#      shared across a group of routes.
#   4. A justification comment may not be byte-identical to the justification
#      immediately before it. "Cannot be shared across a group of routes"
#      (rule 3) was enforced only by position -- a copy-pasted duplicate
#      still counts as each route having "its own" comment line, so it slid
#      past rule 3 (#5762 round 6, F14). Give each route its own wording, even
#      when the underlying reason is the same for both.
if [ -f "$known_drift_file" ]; then
  # Anchored to "^[[:space:]]*#.*" so only comment lines are scanned -- a
  # route path such as "/api/v0/todo-board" or "/api/v0/cache/wipe" must
  # never trip these rules with no escape hatch (#5762 follow-up). The
  # trailing "s?\b" lets the plural "TODOs" match while stopping "WIP" from
  # matching inside an unrelated word like "wipes".
  known_drift_marker_pattern='^[[:space:]]*#.*\b(TO[-_ ]?DO|FIXME|XXX|HACK|TBD|WIP)s?\b'
  known_drift_prose_pattern='^[[:space:]]*#.*(not[[:space:]]+written|written[[:space:]]+yet|not[[:space:]]+yet[[:space:]]+written|\bpending\b|\bpredate[sd]?\b|\blater\b|to be (added|written))'

  # rg exits 1 for "no match" (expected -- most known-drift files have none)
  # and 2 for a hard error (unreadable file, a pattern the installed rg
  # rejects). "|| true" used to swallow both alike, so a hard rg failure
  # silently produced empty hits and let the gate print "OpenAPI surface
  # clean" instead of failing (#5762 round 6, F7). Capture the exit code
  # directly and treat anything above 1 as fatal.
  set +e
  known_drift_deferral_hits="$(rg -in "$known_drift_marker_pattern" "$known_drift_file")"
  known_drift_marker_rc=$?
  known_drift_prose_hits="$(rg -in "$known_drift_prose_pattern" "$known_drift_file")"
  known_drift_prose_rc=$?
  set -e
  if [ "$known_drift_marker_rc" -gt 1 ] || [ "$known_drift_prose_rc" -gt 1 ]; then
    echo "KNOWN-DRIFT SCAN FAILED: ${known_drift_file}"
    echo ""
    echo "rg exited with a hard error (marker scan rc=${known_drift_marker_rc},"
    echo "prose scan rc=${known_drift_prose_rc}) instead of 0 (match) or 1 (no"
    echo "match). Treating this as a gate failure instead of silently reporting"
    echo "a clean surface -- an unreadable file or a pattern rg rejects must not"
    echo "look the same as \"no known-drift entries defer themselves.\""
    exit 1
  fi

  known_drift_unjustified=""
  known_drift_duplicate_justifications=""
  known_drift_prev_justification=""
  known_drift_justified=0
  known_drift_lineno=0
  while IFS= read -r known_drift_line || [ -n "$known_drift_line" ]; do
    known_drift_lineno=$((known_drift_lineno + 1))
    known_drift_trimmed="$(printf '%s' "$known_drift_line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    if [ -z "$known_drift_trimmed" ]; then
      known_drift_justified=0
      continue
    fi
    case "$known_drift_trimmed" in
      '#'*)
        # A justification needs real words: at least two whitespace-separated
        # tokens, one of which is a 4+ letter alphabetic word. A raw
        # character count let pure decoration ("####", "# ---", "# ...")
        # pass as "substantive" (#5762 follow-up P2-1).
        known_drift_comment_text="${known_drift_trimmed#\#}"
        known_drift_comment_tokens="$(printf '%s' "$known_drift_comment_text" | wc -w | tr -d ' ')"
        if [ "$known_drift_comment_tokens" -ge 2 ] \
          && printf '%s' "$known_drift_comment_text" | rg -q '[[:alpha:]]{4,}'; then
          known_drift_justified=1
          # Rule 4 compares MEANING, not bytes: collapse runs of whitespace to
          # one space and trim the ends before comparing, so "#Foo" vs "# Foo"
          # (the leading "#" is stripped above, leaving a leading-space
          # difference) and a single doubled internal space both count as the
          # same justification instead of dodging the check on formatting
          # alone (#5762 follow-up P2-1). This still compares only against
          # the immediately preceding justification -- a non-adjacent
          # A,B,A repeat is not caught; that gap is documented, not silently
          # assumed away, in docs/internal/design/3738-openapi-discipline.md
          # rule 4.
          known_drift_comment_normalized="$(printf '%s' "$known_drift_comment_text" | tr -s '[:space:]' ' ' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
          # A copy-pasted justification still counts as "its own" comment
          # under rule 3's positional check, so a duplicate needs its own
          # rule (#5762 round 6, F14). Compare against the last comment that
          # itself passed rule 3 -- an unjustified comment never updates
          # $known_drift_prev_justification, so it cannot mask a real
          # duplicate two entries later.
          if [ -n "$known_drift_prev_justification" ] \
            && [ "$known_drift_comment_normalized" = "$known_drift_prev_justification" ]; then
            known_drift_duplicate_justifications="${known_drift_duplicate_justifications}${known_drift_file}:${known_drift_lineno}: \"${known_drift_trimmed}\""$'\n'
          fi
          known_drift_prev_justification="$known_drift_comment_normalized"
        else
          known_drift_justified=0
        fi
        ;;
      *)
        if [ "$known_drift_justified" -ne 1 ]; then
          known_drift_unjustified="${known_drift_unjustified}${known_drift_file}:${known_drift_lineno}: \"${known_drift_trimmed}\""$'\n'
        fi
        # A justification comment covers exactly the one route line that
        # follows it -- reset so the next route needs its own comment
        # (#5762 follow-up: a shared comment let a route ride in silently
        # right after an already-justified one).
        known_drift_justified=0
        ;;
    esac
  done < "$known_drift_file"

  if [ -n "$known_drift_deferral_hits" ] || [ -n "$known_drift_prose_hits" ] \
    || [ -n "$known_drift_unjustified" ] || [ -n "$known_drift_duplicate_justifications" ]; then
    echo "KNOWN-DRIFT FILE INVALID: ${known_drift_file}"
    echo ""
    echo "This file is a permanent-exclusion list, not a backlog. An entry must"
    echo "assert \"this route is not OpenAPI,\" never that the fragment is simply"
    echo "missing so far."
    echo ""
    if [ -n "$known_drift_deferral_hits" ]; then
      echo "DEFERRAL_MARKER: a TODO/FIXME/XXX/HACK/TBD/WIP marker means this is a"
      echo "deferred gap, not a permanent exclusion -- give the route a real"
      echo "openapi_paths_*.go entry (or a genuine permanent-exclusion"
      echo "justification) instead:"
      while IFS= read -r hit; do
        echo "  ${known_drift_file}:${hit}"
      done <<< "$known_drift_deferral_hits"
      echo ""
    fi
    if [ -n "$known_drift_prose_hits" ]; then
      echo "PROSE_DEFERRAL: a phrase like \"not written\", \"written yet\","
      echo "\"pending\", \"predates\", \"later\", or \"to be added/written\" is the"
      echo "same deferral claim spelled out in words -- give the route a real"
      echo "openapi_paths_*.go entry (or a genuine permanent-exclusion"
      echo "justification) instead:"
      while IFS= read -r hit; do
        echo "  ${known_drift_file}:${hit}"
      done <<< "$known_drift_prose_hits"
      echo ""
    fi
    if [ -n "$known_drift_unjustified" ]; then
      echo "UNJUSTIFIED_ENTRY: every route entry needs its own preceding"
      echo "substantive comment (at least two words, one of them 4+ letters)"
      echo "explaining why it is excluded -- a bare \"#\" and a comment shared"
      echo "with an earlier route both count as unjustified:"
      while IFS= read -r entry; do
        [ -n "$entry" ] && echo "  ${entry}"
      done <<< "$known_drift_unjustified"
      echo ""
    fi
    if [ -n "$known_drift_duplicate_justifications" ]; then
      echo "DUPLICATE_JUSTIFICATION: a justification comment byte-identical to"
      echo "the one before it is a copy-paste, not its own reason -- give this"
      echo "route its own wording, even if the underlying reason is the same:"
      while IFS= read -r entry; do
        [ -n "$entry" ] && echo "  ${entry}"
      done <<< "$known_drift_duplicate_justifications"
    fi
    exit 1
  fi
fi

known_drift_tmp="${tmpdir}/known_drift.txt"
: > "$known_drift_tmp"
if [ -f "$known_drift_file" ]; then
  # Trim each line the same way the self-validation loop above does, so an
  # indented comment or an indented route is classified consistently by both
  # passes instead of the validator trimming and this consumer not.
  while IFS= read -r known_drift_consumer_line || [ -n "$known_drift_consumer_line" ]; do
    known_drift_consumer_trimmed="$(printf '%s' "$known_drift_consumer_line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    case "$known_drift_consumer_trimmed" in
      '' | '#'*) continue ;;
      *) printf '%s\n' "$known_drift_consumer_trimmed" ;;
    esac
  done < "$known_drift_file" | sort -u > "$known_drift_tmp"
  # Filter known drift out of the handlefunc set so they are treated as
  # intentionally covered.
  if [ -s "$known_drift_tmp" ]; then
    comm -23 "$handlefunc_route_file" "$known_drift_tmp" > "${tmpdir}/handlefunc_filtered.txt"
    mv "${tmpdir}/handlefunc_filtered.txt" "$handlefunc_route_file"
  fi
fi

# ── 2. Extract routes from openapi_paths_*.go files ─────────────────────────
#
# Each file is a Go string constant of JSON shape:
#     "/path": {
#       "get": {

openapi_route_file="${tmpdir}/openapi_routes.txt"

for f in "$query_dir"/openapi_paths_*.go; do
  [ -f "$f" ] || continue
  awk '
    BEGIN { path = ""; depth = 0; path_depth = 0 }
    {
      line = $0
      # Count braces to track nesting depth.
      nc = gsub(/\{/, "&", line)
      depth += nc
      no = gsub(/\}/, "&", line)
      depth -= no
    }
    # When depth drops to or below the path opening depth, clear the path.
    depth <= path_depth { path = ""; path_depth = 0 }
    # Match a path line: leading whitespace, quote, slash-path, quote, colon, brace.
    /^[[:space:]]*"\/[^"]*"[[:space:]]*:[[:space:]]*\{/ {
      raw = $0
      sub(/^[[:space:]]+/, "", raw)
      sub(/^"/, "", raw)
      sub(/"[[:space:]]*:[[:space:]]*\{.*/, "", raw)
      path = raw
      path_depth = depth - 1  # depth after the opening brace
      next
    }
    # Match an HTTP method line inside the current path block.
    path != "" && /^[[:space:]]*"(get|post|put|delete|patch|options)"/ {
      raw_method = $0
      sub(/^[[:space:]]+/, "", raw_method)
      sub(/^"/, "", raw_method)
      sub(/".*/, "", raw_method)
      print toupper(raw_method) " " path
    }
  ' "$f"
done | sort -u > "$openapi_route_file"

# ── 3. Cross-reference both sets ────────────────────────────────────────────

missing_in_openapi="${tmpdir}/missing_in_openapi.txt"
comm -23 "$handlefunc_route_file" "$openapi_route_file" > "$missing_in_openapi" || true

missing_in_handler="${tmpdir}/missing_in_handler.txt"
comm -13 "$handlefunc_route_file" "$openapi_route_file" > "$missing_in_handler" || true

# ── 4. Report ───────────────────────────────────────────────────────────────

handlefunc_count="$(wc -l < "$handlefunc_route_file" | tr -d ' ')"
openapi_count="$(wc -l < "$openapi_route_file" | tr -d ' ')"
missing_openapi_count="$(wc -l < "$missing_in_openapi" | tr -d ' ')"
missing_handler_count="$(wc -l < "$missing_in_handler" | tr -d ' ')"

if [ "$missing_openapi_count" -gt 0 ] || [ "$missing_handler_count" -gt 0 ]; then
  echo "OPENAPI DRIFT DETECTED"
  echo ""
  echo "HandleFunc routes:  $handlefunc_count"
  echo "OpenAPI path entries: $openapi_count"
  echo "Missing from OpenAPI: $missing_openapi_count"
  echo "OpenAPI without handler: $missing_handler_count"
  echo ""

  if [ "$missing_openapi_count" -gt 0 ]; then
    while IFS= read -r route; do
      echo "MISSING_OPENAPI: $route"
    done < "$missing_in_openapi"
  fi
  if [ "$missing_handler_count" -gt 0 ]; then
    while IFS= read -r route; do
      echo "ORPHAN_OPENAPI: $route"
    done < "$missing_in_handler"
  fi
  exit 1
fi

echo "OpenAPI surface clean: $handlefunc_count HandleFunc routes, $openapi_count OpenAPI path entries"
exit 0
