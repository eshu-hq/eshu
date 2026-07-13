#!/usr/bin/env bash
# Renders the "Bundled NornicDB" values example from
# docs/public/deploy/kubernetes/storage.md through `helm template` so a
# copy-pasted doc snippet is proven to render, not just parse as YAML.
# Motivated by #5103: the example set neo4j.auth.secretName: "" with no
# neo4j.auth.password, which the chart's `required` guard in
# templates/_helpers.tpl (rendered from templates/statefulset.yaml) rejects.
# `mkdocs build --strict` does not catch this class of drift because it never
# invokes Helm against the embedded snippet.
#
# Usage:
#   scripts/verify-storage-doc-bundled-nornicdb-example.sh
#
# ESHU_STORAGE_DOC_PATH overrides the doc path for the hermetic self-test in
# scripts/test-verify-storage-doc-bundled-nornicdb-example.sh.
#
# Exit codes:
#   0 — the example renders cleanly.
#   1 — the example is missing, empty, or fails `helm template`.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart="${repo_root}/deploy/helm/eshu"
doc="${ESHU_STORAGE_DOC_PATH:-${repo_root}/docs/public/deploy/kubernetes/storage.md}"

if [[ ! -f "${doc}" ]]; then
	echo "storage doc not found: ${doc}" >&2
	exit 1
fi

snippet="$(mktemp)"
helm_err="$(mktemp)"
cleanup() {
	rm -f "${snippet}" "${helm_err}"
}
trap cleanup EXIT

awk '
  /^## Bundled NornicDB$/ { in_section = 1; next }
  in_section && /^## / { exit }
  in_section && /^```yaml$/ && !started { started = 1; next }
  in_section && started && /^```$/ { exit }
  in_section && started { print }
' "${doc}" >"${snippet}"

if [[ ! -s "${snippet}" ]]; then
	echo "no 'Bundled NornicDB' yaml example found in ${doc}" >&2
	exit 1
fi

if ! helm template eshu-doc-check "${chart}" -f "${snippet}" >/dev/null 2>"${helm_err}"; then
	echo "the 'Bundled NornicDB' example in ${doc} fails 'helm template':" >&2
	cat "${helm_err}" >&2
	exit 1
fi

echo "verify-storage-doc-bundled-nornicdb-example: pass"
