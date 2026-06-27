#!/usr/bin/env bash
#
# verify-edge-source-tool-coverage.sh — fail when an EvidenceKind constant in
# go/internal/relationships/models.go is not classified to a real source_tool
# by the reducer's classifier (go/internal/reducer/cross_repo_evidence_type.go),
# so it would silently fall to "unknown" at write time.
#
# This is the X2 static-analysis gate for edge source_tool provenance coverage
# (issue #4002, epic #3997). Without this script, adding a new EvidenceKind
# constant without a corresponding entry in evidenceKindToSourceTool or a
# matching family prefix in sourceToolPrefixFallback is a silent coverage gap:
# the constant compiles and is persisted, but every edge carrying it is labeled
# "unknown" in the graph.
#
# The gate auto-discovers constants via rg rather than iterating a curated map,
# which is the class of gap the existing Go unit tests cannot close: a new
# constant never appears in a map the test iterates.
#
# Exit 0 on success; non-zero with per-constant fix-hints on coverage drift.
#
# Environment variables (for testing / injection):
#   ESHU_SOURCE_TOOL_MODELS_FILE      — override path to relationships/models.go
#   ESHU_SOURCE_TOOL_CLASSIFIER_FILE  — override path to cross_repo_evidence_type.go
#   ESHU_SOURCE_TOOL_REPO_ROOT        — override repo root (default: script/../)
set -euo pipefail

# ---------------------------------------------------------------------------
# Path resolution
# ---------------------------------------------------------------------------
repo_root="${ESHU_SOURCE_TOOL_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive from the script's own location so the script is worktree-safe and
  # git-hook-safe (GIT_DIR breaks `git rev-parse --show-toplevel`).
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

models_file="${ESHU_SOURCE_TOOL_MODELS_FILE:-${repo_root}/go/internal/relationships/models.go}"
classifier_file="${ESHU_SOURCE_TOOL_CLASSIFIER_FILE:-${repo_root}/go/internal/reducer/cross_repo_evidence_type.go}"

contract_doc="docs/public/reference/edge-source-tool-provenance.md"

# ---------------------------------------------------------------------------
# File existence guards
# ---------------------------------------------------------------------------
if [ ! -f "$models_file" ]; then
  printf 'verify-edge-source-tool-coverage: models file missing: %s\n' "$models_file" >&2
  exit 1
fi
if [ ! -f "$classifier_file" ]; then
  printf 'verify-edge-source-tool-coverage: classifier file missing: %s\n' "$classifier_file" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Temp file setup
# ---------------------------------------------------------------------------
constants_tmp="$(mktemp)"
map_keys_tmp="$(mktemp)"
prefixes_tmp="$(mktemp)"
uncovered_tmp="$(mktemp)"
trap 'rm -f "$constants_tmp" "$map_keys_tmp" "$prefixes_tmp" "$uncovered_tmp"' EXIT

# ---------------------------------------------------------------------------
# 1. Extract every EvidenceKind constant from models.go.
#    Match lines like:
#      EvidenceKindFoo EvidenceKind = "BAR_VALUE"
#    Capture: identifier=EvidenceKindFoo  value=BAR_VALUE
#    Output format: <identifier> <value>
# ---------------------------------------------------------------------------
rg -o 'EvidenceKind\w+ EvidenceKind = "[^"]+"' "$models_file" \
  | sed 's/ EvidenceKind = / /' \
  | tr -d '"' \
  | sort -u >"$constants_tmp" || true

if [ ! -s "$constants_tmp" ]; then
  printf 'verify-edge-source-tool-coverage: no EvidenceKind constants found in %s\n' "$models_file" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# 2. Extract keys present in evidenceKindToSourceTool from the classifier.
#    Lines in the map look like:
#      relationships.EvidenceKindFoo: "tool",
#    or
#      relationships.EvidenceKindFoo:   "tool",
#    We want the bare identifier EvidenceKindFoo.
#
#    CRITICAL: the classifier file ALSO contains the evidenceKindToType map,
#    whose keys use the identical `relationships.EvidenceKindFoo:` syntax. A
#    whole-file scan would treat a kind that is in evidenceKindToType but NOT in
#    evidenceKindToSourceTool as "covered", silently missing exactly the drift
#    this gate exists to block (sourceToolForEvidenceKind would return "unknown"
#    for such a kind). So extract ONLY the evidenceKindToSourceTool map literal —
#    from its `var evidenceKindToSourceTool = map[...]{` line to the closing `}`
#    at column 0 — before pulling keys.
# ---------------------------------------------------------------------------
awk '/^var evidenceKindToSourceTool = map\[/{inblock=1} inblock{print} inblock && /^}/{inblock=0}' "$classifier_file" \
  | rg -o 'relationships\.(EvidenceKind\w+):' \
    --replace '$1' \
  | sort -u >"$map_keys_tmp" || true

if [ ! -s "$map_keys_tmp" ]; then
  printf 'verify-edge-source-tool-coverage: no keys found in evidenceKindToSourceTool in %s\n' "$classifier_file" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# 3. Extract family prefixes from sourceToolPrefixFallback in the classifier.
#    Entries look like:  {"TERRAFORM_", "terraform"},
#    We capture the uppercase prefix string only.
# ---------------------------------------------------------------------------
rg -o '\{"([A-Z_]+_)"' "$classifier_file" \
  --replace '$1' \
  | sort -u >"$prefixes_tmp" || true

if [ ! -s "$prefixes_tmp" ]; then
  printf 'verify-edge-source-tool-coverage: no family prefixes found in sourceToolPrefixFallback in %s\n' "$classifier_file" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# 4. For each constant, determine coverage:
#    COVERED if (a) its Go identifier is a key in evidenceKindToSourceTool, OR
#              (b) its string VALUE starts with one of the family prefixes.
#    Otherwise UNCOVERED.
# ---------------------------------------------------------------------------
: >"$uncovered_tmp"
while IFS=' ' read -r identifier value; do
  [ -n "$identifier" ] || continue
  [ -n "$value" ] || continue

  # (a) Named-constant map lookup.
  if rg -qx "$identifier" "$map_keys_tmp" 2>/dev/null; then
    continue
  fi

  # (b) Family-prefix fallback: check if value starts with any known prefix.
  covered_by_prefix=0
  while IFS= read -r prefix; do
    [ -n "$prefix" ] || continue
    case "$value" in
      "${prefix}"*)
        covered_by_prefix=1
        break
        ;;
    esac
  done <"$prefixes_tmp"

  if [ "$covered_by_prefix" -eq 1 ]; then
    continue
  fi

  # UNCOVERED: record it.
  printf '%s %s\n' "$identifier" "$value" >>"$uncovered_tmp"
done <"$constants_tmp"

# ---------------------------------------------------------------------------
# 5. Report.
# ---------------------------------------------------------------------------
if [ -s "$uncovered_tmp" ]; then
  {
    printf 'verify-edge-source-tool-coverage: source_tool provenance coverage drift detected\n\n'
    printf 'The following EvidenceKind constants are not classified to any source_tool.\n'
    printf 'Edges carrying these kinds will be labeled "unknown" at write time.\n\n'
    printf 'Contract doc: %s\n\n' "$contract_doc"
    printf 'Uncovered constants:\n'
    while IFS=' ' read -r identifier value; do
      [ -n "$identifier" ] || continue
      printf '  - %s (value: "%s")\n' "$identifier" "$value"
      printf '    Fix: add relationships.%s to evidenceKindToSourceTool in\n' "$identifier"
      printf '         %s,\n' "$classifier_file"
      printf '         or ensure its value starts with a prefix in sourceToolPrefixFallback.\n'
    done <"$uncovered_tmp"
  } >&2
  exit 1
fi

constant_count="$(wc -l <"$constants_tmp" | tr -d ' ')"
printf 'verify-edge-source-tool-coverage: all %s EvidenceKind constants are classified to a source_tool (pass)\n' "$constant_count"
