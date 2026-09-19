#!/usr/bin/env bash

# Sourced by test-verify-parser-relationship-kit.sh after
# test-verify-parser-relationship-kit-dsl-comment-only-cases.sh, whose
# dsl_case helper these cases reuse. Kept out of the parent test driver so
# that driver stays below the repository's 500-line cap.
# shellcheck disable=SC2154 # Parent defines fixture helpers and repo paths.

# #6818: a repository-internal package move rewrites the import path and the
# package qualifier in every importer. When that is the file's only change,
# the language-query-source rule must not demand a Language Query DSL doc
# update. The exemption is decided by go/cmd/token-diff
# -allow-internal-import-rename on the token stream; these cases run it end
# to end through the real gate.
rename_base_source() {
  cat <<'GO'
package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
)

// languageTracer is seeded from queryspan.HandlerTracer.
var languageTracer = queryspan.HandlerTracer()

func languageSpan(r *http.Request) {
	queryspan.StartHandlerSpanWith(languageTracer, r, "language", "route")
}
GO
}

rename_head_source() {
  rename_base_source | sed 's/queryspan/tracing/g'
}

# R1: pure import-path plus qualifier rename -- exempt.
dsl_case rename-r1-pure-qualifier \
  "$(rename_base_source)" \
  "$(rename_head_source)" \
  pass

# R2: the same rename plus a real code change in the same file -- NOT exempt.
dsl_case rename-r2-plus-code-change \
  "$(rename_base_source)" \
  "$(rename_head_source | sed 's/"route"/"other-route"/')" \
  fail

# R3: the qualifier renamed inconsistently (two different new names) -- NOT
# exempt.
dsl_case rename-r3-inconsistent-qualifier \
  "$(rename_base_source)" \
  "$(rename_head_source | sed 's/tracing\.StartHandlerSpanWith/tracer.StartHandlerSpanWith/')" \
  fail

# R4: a non-internal import path changes the same way -- NOT exempt; only a
# move under github.com/eshu-hq/eshu/go/internal/ qualifies.
dsl_case rename-r4-non-internal-import \
  "$(rename_base_source | sed 's#github.com/eshu-hq/eshu/go/internal/query/queryspan#example.com/lib/queryspan#')" \
  "$(rename_head_source | sed 's#github.com/eshu-hq/eshu/go/internal/query/tracing#example.com/lib/tracing#')" \
  fail
