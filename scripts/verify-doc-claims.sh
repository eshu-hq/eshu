#!/usr/bin/env bash
# scripts/verify-doc-claims.sh
#
# Slop gate. Confirms that every backticked Go-style identifier in a
# package's README.md and AGENTS.md actually exists in the package's
# source — or in an explicit allowlist of stdlib / project-wide names.
# Also runs an anti-marketing pass on the prose.
#
# Usage:
#   scripts/verify-doc-claims.sh <pkg-dir>
#   scripts/verify-doc-claims.sh --all          # every Go package under go/
#
# Exit codes:
#   0 — clean
#   1 — unverifiable identifier or marketing word found
#   2 — usage error
#
# Why this exists: PR #139 shipped invented identifiers (GraphWrite,
# Server, RuntimeAdminRequest) because agents extrapolated from plausible
# package design instead of grepping for real exports. The parent
# (whoever invokes this script) runs it before approving each package.

set -u
set -o pipefail

repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
  || (cd "$(dirname "$0")/.." && pwd))"

# ---------------------------------------------------------------------------
# Allowlist: identifiers that may appear in docs without living in the
# package's own .go files. Keep this list narrow on purpose — every entry
# is a way for slop to slip through.
# ---------------------------------------------------------------------------
allow_stdlib=(
  Context Reader Writer Closer Handler Server Client Request Response
  Conn DB Tx Stmt Rows Row Scanner Marshaler Unmarshaler Error
  Time Duration Ticker WaitGroup Mutex RWMutex Once
  ErrUnsupported ErrNotExist ErrInvalid
  HTTPHandler ServeMux ResponseWriter
  Logger Slog Attr
  Cancel CancelFunc CancelCauseFunc
  Buffer Reader Writer
)
allow_project=(
  Eshu MCP API SCIP OTEL HTTP JSON TOML YAML SQL CLI ADR
  Postgres Neo4j NornicDB Cypher
  GraphQL OpenAPI
  README AGENTS CLAUDE
  GET POST PUT DELETE PATCH
  Helm Kustomize Argo Terraform Compose
  SIGTERM SIGINT
  GOMEMLIMIT GODEBUG GOMAXPROCS
  # Shared Eshu runtime env vars defined in internal/runtime and consumed by
  # multiple cmd binaries. Backticked references in cmd-local README.md files
  # are correct but the names do not live in cmd-local *.go.
  ESHU_POSTGRES_DSN ESHU_FACT_STORE_DSN ESHU_CONTENT_STORE_DSN
  # HTTP/S3 header prefix tokens referenced in narrative prose (the
  # extractor captures up to the dash, so `If-None-Match` lands as `If`).
  If
  # Test function names cited in package README/AGENTS docs as load-bearing
  # regression gates. The slop gate excludes *_test.go from the source
  # haystack on purpose, so test names must be allowlisted explicitly when
  # docs need to cite the gate by name.
  TestParseStream_PeakMemoryGate_CompositeCapture
)

is_allowlisted() {
  local tok="$1"
  for s in "${allow_stdlib[@]}" "${allow_project[@]}"; do
    [ "$tok" = "$s" ] && return 0
  done
  return 1
}

# ---------------------------------------------------------------------------
# Anti-marketing words. Every hit is a soft fail — agents must rewrite the
# sentence rather than ship marketing-flavored prose.
# ---------------------------------------------------------------------------
marketing_pattern='\b(leverages|leveraging|seamless(ly)?|robust(ly)?|powerful|comprehensive|key role|stands as|serves as|underscores|showcases|facilitates|delve|delves|delving)\b'

# ---------------------------------------------------------------------------
# Strip fenced code blocks so identifiers inside ``` ... ``` are not
# checked. Code examples can legitimately reference any identifier.
# ---------------------------------------------------------------------------
strip_fences() {
  awk '
    /^```/ { in_block = !in_block; next }
    !in_block { print }
  ' "$1"
}

extract_idents() {
  # Capture the leading exported-identifier inside any backticked expression.
  # We match the head identifier even if the backticks contain a dotted form
  # like `EntityRow.FilePath` or a compound like `FileRow.Path` — the head
  # (`EntityRow`, `FileRow`) is what we verify against source. Trailing
  # `.Field` parts can be extension fields, method names, or composed types
  # whose presence in source is implied by the head.
  strip_fences "$1" \
    | rg --only-matching --no-line-number \
        '`[A-Z][A-Za-z0-9_]+' \
    | tr -d '`' \
    | sort -u
}

extract_telemetry_idents() {
  # telemetry references are usually `telemetry.Foo` — capture the Foo
  # part separately so we can check it against internal/telemetry/.
  strip_fences "$1" \
    | rg --only-matching --no-line-number \
        '`telemetry\.([A-Z][A-Za-z0-9_]+)`' --replace '$1' \
    | sort -u
}

extract_file_cites() {
  # Capture backticked path:line cites like `runtime.go:191`, `neo4j.go:34`,
  # or `canonical_builder.go:112`. The character class includes digits so
  # filenames like `neo4j.go` and `client_v2.go` are not silently skipped.
  # Returns one cite per line.
  strip_fences "$1" \
    | rg --only-matching --no-line-number \
        '`[a-z0-9_]+\.go:[0-9]+`' \
    | tr -d '`' \
    | sort -u
}

verify_package() {
  local pkg_dir="$1"
  local readme="$pkg_dir/README.md"
  local agents="$pkg_dir/AGENTS.md"

  local issues=0

  # Source-grep target: every .go file in this package directory only
  # (--max-depth 1 is critical — without it, sibling subpackages bleed in).
  local source_haystack
  source_haystack=$(rg --files --max-depth 1 \
    -g '*.go' -g '!*_test.go' "$pkg_dir" 2>/dev/null)

  if [ -z "$source_haystack" ] && [ -f "$readme" ]; then
    # Container directory (e.g. go/cmd, go/internal) — only check
    # marketing pass, no Go source to ground identifiers against.
    check_marketing "$readme" "$pkg_dir"
    return $?
  fi

  for doc in "$readme" "$agents"; do
    [ -f "$doc" ] || continue

    # Marketing pass.
    if rg -i -n -e "$marketing_pattern" "$doc" >/dev/null 2>&1; then
      printf '%s: marketing words detected:\n' "$doc" >&2
      rg -i -n -e "$marketing_pattern" "$doc" >&2 || true
      issues=$((issues + 1))
    fi

    # Telemetry idents must live in internal/telemetry/ source.
    while IFS= read -r tel; do
      [ -z "$tel" ] && continue
      if ! rg -q --no-messages "\b$tel\b" \
          "$repo_root/go/internal/telemetry" 2>/dev/null; then
        printf '%s: telemetry.%s not found in internal/telemetry/\n' \
          "$doc" "$tel" >&2
        issues=$((issues + 1))
      fi
    done < <(extract_telemetry_idents "$doc")

    # File:line cites must resolve to the right neighborhood. Three checks:
    # (a) file exists in this package
    # (b) cited line is within the file
    # (c) at least one identifier from the same doc paragraph as the cite
    #     appears within ±10 lines of the cited line in the source.
    # (c) catches the case where the file is real and the line is in range
    # but the citation has drifted from the symbol it was originally about.
    while IFS= read -r cite; do
      [ -z "$cite" ] && continue
      local cite_file="${cite%%:*}"
      local cite_line="${cite##*:}"
      local cite_path="$pkg_dir/$cite_file"
      if [ ! -f "$cite_path" ]; then
        printf '%s: file cite `%s` — file %s not found\n' \
          "$doc" "$cite" "$cite_path" >&2
        issues=$((issues + 1))
        continue
      fi
      local total_lines
      total_lines=$(wc -l <"$cite_path" | tr -d ' ')
      if [ "$cite_line" -gt "$total_lines" ]; then
        printf '%s: file cite `%s` — line %d past EOF (%d lines)\n' \
          "$doc" "$cite" "$cite_line" "$total_lines" >&2
        issues=$((issues + 1))
        continue
      fi

      # Extract the paragraph in the doc that contains this cite.
      # awk reads the doc, tracks blank-line boundaries, and prints the
      # paragraph that contains the cite token verbatim.
      local paragraph
      paragraph=$(awk -v needle="$cite" '
        BEGIN { para = "" }
        /^[[:space:]]*$/ {
          if (para ~ needle) { print para; exit }
          para = ""; next
        }
        { para = para "\n" $0 }
        END { if (para ~ needle) print para }
      ' "$doc")

      # Find backticked CamelCase identifiers in that paragraph (head of any
      # dotted form), excluding allowlisted/stdlib names.
      local idents_in_para
      idents_in_para=$(printf '%s' "$paragraph" \
        | rg --only-matching --no-line-number '`[A-Z][A-Za-z0-9_]+' \
        | tr -d '`' | sort -u)

      [ -z "$idents_in_para" ] && continue   # no anchor identifiers

      # Compute ±10 line range in the source.
      local lo=$((cite_line - 10))
      local hi=$((cite_line + 10))
      [ "$lo" -lt 1 ] && lo=1
      local source_window
      source_window=$(sed -n "${lo},${hi}p" "$cite_path" 2>/dev/null)

      local anchor_found=0
      while IFS= read -r tok; do
        [ -z "$tok" ] && continue
        is_allowlisted "$tok" && continue
        if printf '%s' "$source_window" | rg -q "\b$tok\b" 2>/dev/null; then
          anchor_found=1
          break
        fi
      done <<<"$idents_in_para"

      if [ "$anchor_found" -eq 0 ]; then
        printf '%s: file cite `%s` — no paragraph identifier found within ±10 lines of %s:%s\n' \
          "$doc" "$cite" "$cite_path" "$cite_line" >&2
        issues=$((issues + 1))
      fi
    done < <(extract_file_cites "$doc")

    # Generic Go identifiers must live in this package's source files
    # OR be allowlisted.
    while IFS= read -r tok; do
      [ -z "$tok" ] && continue
      is_allowlisted "$tok" && continue
      local found=0
      while IFS= read -r f; do
        if rg -q --no-messages "\b$tok\b" "$f" 2>/dev/null; then
          found=1
          break
        fi
      done <<<"$source_haystack"
      if [ "$found" -eq 0 ]; then
        printf '%s: backticked `%s` not found in %s/*.go\n' \
          "$doc" "$tok" "$pkg_dir" >&2
        issues=$((issues + 1))
      fi
    done < <(extract_idents "$doc")
  done

  if [ "$issues" -eq 0 ]; then
    printf 'verify-doc-claims: %s OK\n' "$pkg_dir"
    return 0
  fi
  printf 'verify-doc-claims: %s FAIL (%d issues)\n' "$pkg_dir" "$issues" >&2
  return 1
}

check_marketing() {
  local doc="$1"
  local label="$2"
  if rg -i -n -e "$marketing_pattern" "$doc" >/dev/null 2>&1; then
    printf '%s: marketing words detected:\n' "$label" >&2
    rg -i -n -e "$marketing_pattern" "$doc" >&2 || true
    return 1
  fi
  printf 'verify-doc-claims: %s OK (marketing pass only)\n' "$label"
  return 0
}

main() {
  if [ $# -eq 0 ]; then
    printf 'usage: %s <pkg-dir> | --all\n' "$0" >&2
    exit 2
  fi

  if [ "$1" = "--all" ]; then
    local fail=0
    while IFS= read -r pkg; do
      verify_package "$pkg" || fail=1
    done < <(rg --files --max-depth 99 \
      -g '*.go' -g '!*_test.go' \
      "$repo_root/go" 2>/dev/null \
      | xargs -n1 dirname | sort -u)
    return "$fail"
  fi

  verify_package "$1"
}

main "$@"
