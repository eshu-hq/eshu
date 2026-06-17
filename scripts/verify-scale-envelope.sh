#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
doc="${repo_root}/docs/public/reference/local-performance-envelope.md"

require_doc_text() {
  local pattern="$1"
  local description="$2"
  if ! rg -q --fixed-strings "$pattern" "$doc"; then
    printf 'verify-scale-envelope: missing %s in %s\n' "$description" "$doc" >&2
    exit 1
  fi
}

require_doc_regex() {
  local pattern="$1"
  local description="$2"
  if ! rg -q -e "$pattern" "$doc"; then
    printf 'verify-scale-envelope: missing %s in %s\n' "$description" "$doc" >&2
    exit 1
  fi
}

require_doc_text '## Next-Phase Scale Envelope (#2696)' 'issue #2696 section'
require_doc_text '### Reducer Conflict-Domain Audit (#2697)' 'issue #2697 audit'
require_doc_text '### Postgres Fact And Queue Growth Envelope (#2698)' 'issue #2698 envelope'
require_doc_text '### Collector Fairness And Provider Backpressure (#2699)' 'issue #2699 envelope'
require_doc_text 'No-Regression Evidence:' 'performance evidence marker'
require_doc_text 'No-Observability-Change:' 'observability evidence marker'
require_doc_text 'safe domains' 'safe-domain classification'
require_doc_text 'risky domains' 'risky-domain classification'
require_doc_text 'blocked domains' 'blocked-domain classification'
require_doc_text 'local/dev' 'local/dev profile gate'
require_doc_text 'hosted-small' 'hosted-small profile gate'
require_doc_text 'hosted-growth' 'hosted-growth profile gate'
require_doc_text 'pending' 'pending queue evidence'
require_doc_text 'retrying' 'retrying queue evidence'
require_doc_text 'dead-letter' 'dead-letter queue evidence'
require_doc_text 'provider throttle' 'provider throttle signal'
require_doc_text 'claim wait' 'claim wait signal'
require_doc_text 'lease age' 'lease age signal'
require_doc_text 'per-family queue depth' 'per-family queue depth signal'

if rg -n -e '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b' "$doc"; then
  printf 'verify-scale-envelope: raw IP address found in scale envelope doc\n' >&2
  exit 1
fi

printf 'verify-scale-envelope: scale envelope doc contract passed\n'
