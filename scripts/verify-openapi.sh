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
  find "$dir" -maxdepth 1 -name '*.go' \
    ! -name '*_test.go' \
    ! -name 'openapi_*.go' \
    2>/dev/null \
  >> "$gofiles_tmp"
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
known_drift_tmp="${tmpdir}/known_drift.txt"
: > "$known_drift_tmp"
if [ -f "$known_drift_file" ]; then
  grep -v '^#' "$known_drift_file" | grep -v '^$' | sort -u > "$known_drift_tmp" || true
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
